// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	"bytes"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mholt/archiver/v3"
)

var (
	// The default listening port
	PORT = "80"

	// A cache directory for base runtimes
	RT_CACHE = filepath.FromSlash(os.TempDir() + "/runtime_cache")

	// A directory for short-lived files
	TMP = os.TempDir()

	// Whether Maven Central integration is enabled
	MAVEN_CENTRAL = false

	// The platform for local runtimes
	LOCAL_PLATFORM = determineLocalPlatform()

	// The architecture for local runtimes
	LOCAL_ARCH = "x64"
)

// A client for downloading artifacts and release metadata from api.adoptopenjdk.net
var adoptium = &http.Client{
	Timeout: time.Second * 120,
}

// A client for downloading Maven Central artifacts
var mavenCentral = &http.Client{
	Timeout: time.Second * 60,
}

// RuntimeRequest represents an incoming request from the JSON endpoint.
type runtimeRequest struct {

	// Maven Central artifacts in G:A:V format
	Artifacts []string `json:"artifacts"`

	// The modules to include in the runtime
	Modules []string `json:"modules"`

	// The output endian type
	Endian string `json:"endian"`

	// The Java version
	Version string `json:"version"`

	// The OS type
	Platform string `json:"os"`

	// The architecture type
	Arch string `json:"arch"`

	// The implementation type
	Implementation string `json:"implementation"`
}

func main() {

	router := gin.Default()

	// Override environment variables
	if port, exists := os.LookupEnv("PORT"); exists {
		PORT = port
	}
	if maven_central, exists := os.LookupEnv("MAVEN_CENTRAL"); exists {
		if b, err := strconv.ParseBool(maven_central); err == nil {
			MAVEN_CENTRAL = b
		} else {
			log.Fatal("Invalid value for MAVEN_CENTRAL flag")
		}
	}
	if arch, exists := os.LookupEnv("LOCAL_ARCH"); exists {
		LOCAL_ARCH = arch
	}
	if cache, exists := os.LookupEnv("RT_CACHE"); exists {
		RT_CACHE = cache
	}
	if tmp, exists := os.LookupEnv("TMP"); exists {
		TMP = tmp
	}
	_ = os.MkdirAll(RT_CACHE, os.ModePerm)
	_ = os.MkdirAll(TMP, os.ModePerm)

	// Redirect index requests to the GitHub project page
	router.GET("/", func(context *gin.Context) {
		context.Redirect(http.StatusMovedPermanently, "https://github.com/AdoptOpenJDK/jlink.online")
	})

	// An endpoint for runtime requests
	router.GET("/:arch/:os/:version", func(context *gin.Context) {

		var (
			arch     = context.Param("arch")
			endian   = context.Query("endian")
			impl     = context.DefaultQuery("implementation", "hotspot")
			platform = context.Param("os")
			version  = context.Param("version")
			modules  = strings.Split(context.DefaultQuery("modules", "java.base"), ",")
		)

		var artifacts []string
		if a := context.Query("artifacts"); a != "" {
			if MAVEN_CENTRAL {
				artifacts = strings.Split(a, ",")
			} else {
				context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Maven Central integration is disabled"})
				return
			}
		}

		handleRequest(context, platform, arch, version, endian, impl, modules, artifacts)
	})

	// An endpoint for runtime requests (JSON)
	router.POST("/", func(context *gin.Context) {
		var req runtimeRequest

		err := context.BindJSON(&req)
		if err != nil {
			return
		}

		if !MAVEN_CENTRAL && len(req.Artifacts) > 0 {
			context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Maven Central integration is disabled"})
			return
		}

		handleRequest(context, req.Platform, req.Arch, req.Version, req.Endian, req.Implementation, req.Modules, req.Artifacts)
	})

	// An endpoint for runtime requests containing a module-info.java file
	router.POST("/:arch/:os/:version", func(context *gin.Context) {
		bytes, err := context.GetRawData()
		if err != nil {
			context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "The request body must be a valid module-info.java file"})
			return
		}

		var (
			arch     = context.Param("arch")
			endian   = context.Query("endian")
			impl     = context.DefaultQuery("implementation", "hotspot")
			platform = context.Param("os")
			version  = context.Param("version")
		)

		var artifacts []string
		if a := context.Query("artifacts"); a != "" {
			if MAVEN_CENTRAL {
				artifacts = strings.Split(a, ",")
			} else {
				context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Maven Central integration is disabled"})
				return
			}
		}

		handleRequest(context, platform, arch, version, endian, impl, parseModuleInfo(string(bytes)), artifacts)
	})

	router.Run(":" + PORT)
}

var (
	archCheck     = regexp.MustCompile(`^(x64|x32|ppc64|s390x|ppc64le|aarch64|arm)$`)
	artifactCheck = regexp.MustCompile(`^[\w\.-]+:[\w\.-]+:[\w\.-]+$`)
	moduleCheck   = regexp.MustCompile(`^[\w\.]+$`)
	platformCheck = regexp.MustCompile(`^(linux|windows|mac|solaris|aix)$`)
	versionCheck  = regexp.MustCompile(`^[1-9][0-9]*((\.0)*\.[1-9][0-9]*)*(\+[1-9][0-9]*((\.0)*\.[1-9][0-9]*)*)?$`)
)

func handleRequest(context *gin.Context, platform, arch, version, endian, implementation string, modules, artifacts []string) {

	// Validate platform type
	if !platformCheck.MatchString(platform) {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Valid operating systems: [windows, linux, mac, solaris, aix]"})
		return
	}

	// Validate architecture type
	if !archCheck.MatchString(arch) {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Valid architectures: [x64, x32, ppc64, s390x, ppc64le, aarch64, arm]"})
		return
	}

	// Validate artifacts
	for _, artifact := range artifacts {
		if !artifactCheck.MatchString(artifact) {
			context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Invalid artifact"})
			return
		}
	}

	// Validate modules
	for _, module := range modules {
		if !moduleCheck.MatchString(module) {
			context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Invalid module"})
			return
		}
	}

	// Validate endian type
	if endian == "" {
		// Guess according to supplied architecture
		if arch == "ppc64" || arch == "s390x" {
			endian = "big"
		} else {
			endian = "little"
		}
	}
	if endian != "big" && endian != "little" {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Valid endian types: [little, big]"})
		return
	}

	// Validate implementation
	if implementation != "hotspot" && implementation != "openj9" {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Valid implementation types: [hotspot, openj9]"})
		return
	}

	// Validate version number
	if !versionCheck.MatchString(version) {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Invalid Java version"})
		return
	}

	// Validate major version number
	majorVersion, err := getMajorVersion(version)
	if err != nil || majorVersion < 9 {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Invalid Java version"})
		return
	}

	// Lookup the target runtime whose modules will be packaged into a new runtime image
	target, err := lookupRelease(arch, platform, implementation, version)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to find target runtime"})
		return
	}

	// Lookup a runtime containing a compatible version of jlink for local use
	local, err := lookupRelease(LOCAL_ARCH, LOCAL_PLATFORM, implementation, version)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to find local runtime"})
		return
	}

	// Download the local runtime
	localRuntimePath, err := downloadRelease(local, version)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to download local runtime"})
		log.Println(err)
		return
	}

	// Download the target runtime
	targetRuntimePath, err := downloadRelease(target, version)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to download target runtime"})
		log.Println(err)
		return
	}

	// Create a directory for Maven Central artifacts
	mavenCentral, dir := newTemporaryDirectory("mavenCentral")
	defer os.RemoveAll(dir)

	// Download any required artifacts
	if err := downloadArtifacts(mavenCentral, artifacts); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to download Maven Central artifacts"})
		log.Println(err)
		return
	}

	// Run jlink on the target runtime
	archive, err := jlink(localRuntimePath, mavenCentral, targetRuntimePath, endian, version, platform, target.Package.Name, modules)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to generate runtime"})
		log.Println(err)
		return
	}

	context.DataFromReader(http.StatusOK, int64(archive.Len()), "application/octet-stream", archive, map[string]string{
		"Content-Disposition": "attachment; filename=\"" + target.Package.Name + "\"",
		"Accept-Ranges":       "bytes",
	})
}

// Jlink uses a standard JDK runtime to generate a custom runtime image
// for the given set of modules.
func jlink(jdk, mavenCentral, runtime, endian, version, platform, filename string, modules []string) (*bytes.Buffer, error) {

	var modulePath, jlink string

	// Add the base module if it's not there
	base := false
	for _, m := range modules {
		if m == "java.base" {
			base = true
			break
		}
	}
	if !base {
		modules = append(modules, "java.base")
	}

	output, dir := newTemporaryFile("jdk-" + version)
	defer os.RemoveAll(dir)

	// Build module path according to target platform
	switch platform {
	case "mac":
		_, err := os.Stat(filepath.FromSlash(runtime + "/Contents/Home/jmods"))
		if err != nil {
			return nil, err
		}

		modulePath = filepath.FromSlash(runtime + "/Contents/Home/jmods" + string(os.PathListSeparator) + mavenCentral)
	case "windows":
		_, err := os.Stat(filepath.FromSlash(runtime + "/jmods"))
		if err != nil {
			return nil, err
		}

		modulePath = filepath.FromSlash(runtime + "/jmods" + string(os.PathListSeparator) + mavenCentral)
	default:
		_, err := os.Stat(filepath.FromSlash(runtime + "/jmods"))
		if err != nil {
			return nil, err
		}

		modulePath = filepath.FromSlash(runtime + "/jmods" + string(os.PathListSeparator) + mavenCentral)
	}

	// Build jlink command according to local platform
	switch LOCAL_PLATFORM {
	case "mac":
		jlink = filepath.FromSlash(jdk + "/Contents/Home/bin/jlink")
	case "windows":
		jlink = filepath.FromSlash(jdk + "/bin/jlink.exe")
	default:
		jlink = filepath.FromSlash(jdk + "/bin/jlink")
	}

	if err := os.Chmod(jlink, os.ModePerm); err != nil {
		return nil, err
	}

	cmd := exec.Command(jlink,
		// Share string constants
		"--compress=1",
		// Exclude headers
		"--no-header-files",
		// Exclude man pages
		"--no-man-pages",
		// Remove debug information
		"--strip-debug",
		// The target endian-ness
		"--endian", endian,
		// The path where modules can be found
		"--module-path", modulePath,
		// The selected modules
		"--add-modules", strings.Join(modules, ","),
		// The output directory
		"--output", output)

	log.Println("JLINK:", cmd.Args)
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	archive, dir := newTemporaryFile(filename)
	defer os.RemoveAll(dir)

	// TODO: archiver can't handle symlinks in this directory
	_ = os.RemoveAll(filepath.FromSlash(output + "/legal"))

	if err := archiver.Archive([]string{output}, archive); err != nil {
		return nil, err
	}

	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var buffer bytes.Buffer
	buffer.ReadFrom(f)

	return &buffer, nil
}

func determineLocalPlatform() string {
	var localPlatform string
	if platform, exists := os.LookupEnv("LOCAL_PLATFORM"); exists {
		localPlatform = platform
	} else {
		// For a list of possible values, run: go tool dist list
		localPlatform = runtime.GOOS
	}

	// See https://api.adoptopenjdk.net/swagger-ui/#/Binary/get_v3_binary_latest__feature_version___release_type___os___arch___image_type___jvm_impl___heap_size___vendor_
	// for a list of accepted platforms by AdoptOpenJDK.
	switch localPlatform {
	case "darwin":
		return "mac"
	default:
		return localPlatform
	}
}
