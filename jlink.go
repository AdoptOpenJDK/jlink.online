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
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	// The default listening port
	port = "80"

	// A cache directory for base runtimes
	cache = os.TempDir() + "/jlink/cache"

	// A temporary directory for downloads and output
	tmp = os.TempDir() + "/jlink/tmp"
)

// A client for downloading AdoptOpenJDK releases
var github = &http.Client{
	Timeout: time.Second * 60,
}

// A client for querying release versions
var adoptOpenJdk = &http.Client{
	Timeout: time.Second * 5,
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
	if _port, exists := os.LookupEnv("PORT"); exists {
		port = _port
	}
	if _cache, exists := os.LookupEnv("CACHE"); exists {
		cache = _cache
	}
	if _tmp, exists := os.LookupEnv("TMP"); exists {
		tmp = _tmp
	}
	_ = os.MkdirAll(cache, os.ModePerm)
	_ = os.MkdirAll(tmp, os.ModePerm)

	// Redirect index requests to the GitHub project page
	router.GET("/", func(context *gin.Context) {
		context.Redirect(http.StatusMovedPermanently, "https://github.com/cilki/jlink.online")
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
			artifacts = strings.Split(a, ",")
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
			artifacts = strings.Split(a, ",")
		}

		handleRequest(context, platform, arch, version, endian, impl, parseModuleInfo(string(bytes)), artifacts)
	})

	router.Run(":" + port)
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
		release, _ = lookupLatestRelease(arch, platform, implementation, 11)
		jdkRelease, _ = lookupLatestRelease("x64", "linux", implementation, 11)
	case "ea":
		release, _ = lookupLatestRelease(arch, platform, implementation, 14)
		jdkRelease, _ = lookupLatestRelease("x64", "linux", implementation, 14)
	case "ga":
		release, _ = lookupLatestRelease(arch, platform, implementation, 13)
		jdkRelease, _ = lookupLatestRelease("x64", "linux", implementation, 13)
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
	jdk, err := downloadRuntime(jdkRelease)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to download runtime"})
		log.Println(err)
		return
	}

	// Download the target runtime
	runtime, err := downloadRuntime(release)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to download target runtime"})
		log.Println(err)
		return
	}

	// Choose a temporary directory for the request
	temp := tmp + "/" + strconv.Itoa(rand.Int())
	_ = os.MkdirAll(temp+"/m2", os.ModePerm)
	defer os.RemoveAll(temp)

	// Download any required artifacts
	if err := downloadArtifacts(temp+"/m2", artifacts); err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to download artifacts"})
		log.Println(err)
		return
	}

	// Run jlink on the downloaded runtime to produce an archive
	archive, err := jlink(jdk, temp, runtime, endian, modules, release)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false, "reason": "Failed to generate runtime"})
		log.Println(err)
		return
	}

	info, err := os.Stat(archive)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false})
		log.Println(err)
		return
	}

	reader, err := os.Open(archive)
	if err != nil {
		context.JSON(http.StatusBadRequest, gin.H{"success": false})
		log.Println(err)
		return
	}
	defer reader.Close()

	addHeaders := map[string]string{
		"Content-Disposition": "attachment; filename=\"" + filepath.Base(archive) + "\"",
		"Accept-Ranges":       "bytes",
	}
	context.DataFromReader(http.StatusOK, info.Size(), "application/octet-stream", reader, addHeaders)
}

// Only one runtime can be downloaded at a time. This is to prevent issues with
// partial downloads.
var downloadLock sync.Mutex

// DownloadRuntime downloads a JDK build from AdoptOpenJDK into the system
// temporary directory according to the given parameters.
func downloadRuntime(release *releaseBinary) (string, error) {
	downloadLock.Lock()
	defer downloadLock.Unlock()

	runtime := cache + "/" + strings.TrimSuffix(strings.TrimSuffix(release.FileName, ".zip"), ".tar.gz")
	archive := tmp + "/" + release.FileName

	// Check if the runtime is cached
	if _, e := os.Stat(runtime); !os.IsNotExist(e) {
		return runtime, nil
	}

	// Download the runtime
	if err := download(github, release.Link, archive); err != nil {
		return "", err
	}

	// Create the runtime directory
	if err := os.MkdirAll(runtime, os.ModePerm); err != nil {
		return "", err
	}

	// Extract to the runtime directory
	if err := run("bsdtar", "-C", runtime, "-xf", archive, "-s", "|[^/]*/||"); err != nil {
		return "", err
	}

	// Delete archive
	return runtime, os.Remove(archive)
}

// Jlink uses a standard JDK runtime to generate a custom runtime image
// for the given set of modules.
func jlink(jdk, temp, runtime, endian string, modules []string, release *releaseBinary) (string, error) {
	output := temp + "/jdk-" + release.ReleaseVersion.Version
	archive := temp + "/" + release.FileName

	err := run(jdk+"/bin/jlink", "--compress=0", "--no-header-files", "--no-man-pages", "--endian", endian, "--module-path", runtime+"/jmods:"+temp+"/m2", "--add-modules", strings.Join(modules, ","), "--output", output)
	if err != nil {
		return "", err
	}

	// Create archive
	if err := run("bsdtar", "-C", filepath.Dir(output), "-a", "-cf", archive, filepath.Base(output)); err != nil {
		return "", err
	}

	// Delete runtime
	return archive, os.RemoveAll(output)
}
