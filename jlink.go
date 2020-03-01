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
	"regexp"
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
	RT_CACHE = "/runtime_cache"

	// A memory filesystem for short-lived files
	TMP = "/tmp"

	// The current EA major version
	VERSION_EA = 14

	// The current GA major version
	VERSION_GA = 13

	// The current LTS major version
	VERSION_LTS = 11

	// Whether Maven Central integration is enabled
	MAVEN_CENTRAL = false
)

// A client for downloading AdoptOpenJDK releases
var github = &http.Client{
	Timeout: time.Second * 60,
}

// A client for querying release metadata from api.adoptopenjdk.net
var adoptOpenJdk = &http.Client{
	Timeout: time.Second * 10,
}

// A client for downloading Maven Central artifacts
var mavenCentral = &http.Client{
	Timeout: time.Second * 30,
}

// RuntimeReq represents an incoming request from the JSON endpoint.
type runtimeReq struct {

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
	if cache, exists := os.LookupEnv("CACHE"); exists {
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
		var req runtimeReq

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
	archCheck     = regexp.MustCompile(`^(x64|x32|ppc64|s390x|ppc64le|aarch64|arm32)$`)
	artifactCheck = regexp.MustCompile(`^[\w\.-]+:[\w\.-]+:[\w\.-]+$`)
	moduleCheck   = regexp.MustCompile(`^[\w\.]+$`)
	platformCheck = regexp.MustCompile(`^(linux|windows|mac|solaris|aix)$`)
	versionCheck  = regexp.MustCompile(`^[1-9][0-9]*((\.0)*\.[1-9][0-9]*)*$`)
)

func handleRequest(context *gin.Context, platform, arch, version, endian, implementation string, modules, artifacts []string) {

	// Validate platform type
	if !platformCheck.MatchString(platform) {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Valid operating systems: [windows, linux, mac, solaris, aix]"})
		return
	}

	// Validate architecture type
	if !archCheck.MatchString(arch) {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Valid architectures: [x64, x32, ppc64, s390x, ppc64le, aarch64, arm32]"})
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
	if len(modules) < 1 {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "At least one module is required"})
		return
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

	var release *releaseBinary
	var jdkRelease *releaseBinary

	switch version {
	case "lts":
		release, _ = lookupLatestRelease(arch, platform, implementation, VERSION_LTS)
		jdkRelease, _ = lookupLatestRelease("x64", "linux", implementation, VERSION_LTS)
	case "ea":
		release, _ = lookupLatestRelease(arch, platform, implementation, VERSION_EA)
		jdkRelease, _ = lookupLatestRelease("x64", "linux", implementation, VERSION_EA)
	case "ga":
		release, _ = lookupLatestRelease(arch, platform, implementation, VERSION_GA)
		jdkRelease, _ = lookupLatestRelease("x64", "linux", implementation, VERSION_GA)
	default:
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

		release, _ = lookupRelease(arch, platform, implementation, version)
		jdkRelease, _ = lookupRelease("x64", "linux", implementation, version)
	}

	if release == nil || jdkRelease == nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to find release"})
		return
	}

	// Download a linux runtime containing a compatible version of jlink
	jdk, err := downloadRelease(jdkRelease)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to download runtime"})
		log.Println(err)
		return
	}

	// Download the target runtime
	runtime, err := downloadRelease(release)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to download target runtime"})
		log.Println(err)
		return
	}

	// Create a directory for Maven artifacts
	m2, dir := newTemporaryDirectory("m2")
	defer os.RemoveAll(dir)

	// Download any required artifacts
	if err := downloadArtifacts(m2, artifacts); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to download artifacts"})
		log.Println(err)
		return
	}

	// Run jlink on the downloaded runtime
	archive, err := jlink(jdk, m2, runtime, endian, modules, release)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to generate runtime"})
		log.Println(err)
		return
	}

	context.DataFromReader(http.StatusOK, int64(archive.Len()), "application/octet-stream", archive, map[string]string{
		"Content-Disposition": "attachment; filename=\"" + release.FileName + "\"",
		"Accept-Ranges":       "bytes",
	})
}

// Jlink uses a standard JDK runtime to generate a custom runtime image
// for the given set of modules.
func jlink(jdk, m2, runtime, endian string, modules []string, release *releaseBinary) (*bytes.Buffer, error) {

	if err := os.Chmod(jdk+"/bin/jlink", os.ModePerm); err != nil {
		return nil, err
	}

	output, dir := newTemporaryFile("jdk-" + release.ReleaseVersion.Version)
	defer os.RemoveAll(dir)

	cmd := exec.Command(jdk+"/bin/jlink", "--compress=0", "--no-header-files", "--no-man-pages", "--endian", endian, "--module-path", runtime+"/jmods:"+m2, "--add-modules", strings.Join(modules, ","), "--output", output)
	log.Println("Executing:", cmd.Args)
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	archive, dir := newTemporaryFile(release.FileName)
	defer os.RemoveAll(dir)

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
