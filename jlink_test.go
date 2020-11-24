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
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"

	"github.com/mholt/archiver/v3"
	"github.com/stretchr/testify/assert"
)

// Execute the "java --version" command on the generated runtime
func assertJavaVersion(t *testing.T, output, version, platform string) {

	checkOpenjdkVersion := regexp.MustCompile("(?m)AdoptOpenJDK \\(build " + regexp.QuoteMeta(version) + "\\)")

	if LOCAL_PLATFORM == platform {
		switch platform {
		case "windows":
			out, err := exec.Command(filepath.FromSlash(output+"/jdk-"+version+"/bin/java.exe"), "--version").Output()
			assert.NoError(t, err)

			assert.True(t, checkOpenjdkVersion.MatchString(string(out)))
		default:
			out, err := exec.Command(filepath.FromSlash(output+"/jdk-"+version+"/bin/java"), "--version").Output()
			assert.NoError(t, err)

			assert.True(t, checkOpenjdkVersion.MatchString(string(out)))
		}
	}
}

// Ensure several important files are present in the generated runtime
func assertRuntimeContents(t *testing.T, output, version, platform string) {

	switch platform {
	case "windows":
		_, err := os.Stat(filepath.FromSlash(output + "/jdk-" + version + "/bin/java.exe"))
		assert.NoError(t, err)
	default:
		_, err := os.Stat(filepath.FromSlash(output + "/jdk-" + version + "/bin/java"))
		assert.NoError(t, err)
	}

	if determineLocalPlatform() != "windows" {
		_, err := os.Stat(filepath.FromSlash(output + "/jdk-" + version + "/legal"))
		assert.NoError(t, err)
	}
}

// Ensure no files are present from the wrong platform
func assertNoCrossPlatformFiles(t *testing.T, output, platform string) {

	checkWindowsExclusive := regexp.MustCompile("\\.(dll|exe)$")
	checkMacExclusive := regexp.MustCompile("\\.(dylib)$")
	checkLinuxExclusive := regexp.MustCompile("\\.(so)$")

	err := filepath.Walk(output, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		switch platform {
		case "windows":
			assert.False(t, checkMacExclusive.MatchString(info.Name()))
			assert.False(t, checkLinuxExclusive.MatchString(info.Name()))
		case "linux":
			assert.False(t, checkMacExclusive.MatchString(info.Name()))
			assert.False(t, checkWindowsExclusive.MatchString(info.Name()))
		case "mac":
			assert.False(t, checkWindowsExclusive.MatchString(info.Name()))
			assert.False(t, checkLinuxExclusive.MatchString(info.Name()))
		}
		return nil
	})
	assert.NoError(t, err)
}

func assertRequestSuccess(t *testing.T, req, version, platform string) {
	res, err := http.Get(req)

	assert.NoError(t, err)
	assert.Equal(t, 200, res.StatusCode)
	defer res.Body.Close()

	var archive string
	switch platform {
	case "windows":
		var dir string
		archive, dir = newTemporaryFile("jdk.zip")
		defer os.RemoveAll(dir)
	default:
		var dir string
		archive, dir = newTemporaryFile("jdk.tar.gz")
		defer os.RemoveAll(dir)
	}

	out, err := os.Create(archive)
	assert.NoError(t, err)
	defer out.Close()

	_, err = io.Copy(out, res.Body)
	assert.NoError(t, err)

	output, dir := newTemporaryFile("output")
	defer os.RemoveAll(dir)

	// Extract and sanity check contents
	if err := archiver.Unarchive(archive, output); err != nil {
		assert.NoError(t, err)
	}

	assertRuntimeContents(t, output, version, platform)
	assertNoCrossPlatformFiles(t, output, platform)
	assertJavaVersion(t, output, version, platform)
}

func assertRequestFailure(t *testing.T, req string, expectedCode int) {
	res, err := http.Get(req)

	assert.NoError(t, err)
	assert.Equal(t, expectedCode, res.StatusCode)
}

func TestApi(t *testing.T) {
	os.Setenv("PORT", "8080")
	go main()

	// Allow the server some time to start
	time.Sleep(4 * time.Second)

	// Send valid requests
	assertRequestSuccess(t, "http://localhost:8080/runtime/x64/linux/11.0.8+10?modules=java.base", "11.0.8+10", "linux")
	assertRequestSuccess(t, "http://localhost:8080/runtime/x64/windows/11.0.8+10?modules=java.base", "11.0.8+10", "windows")
	assertRequestSuccess(t, "http://localhost:8080/runtime/x64/mac/11.0.8+10?modules=java.base", "11.0.8+10", "mac")
	assertRequestSuccess(t, "http://localhost:8080/runtime/ppc64/aix/11.0.8+10?modules=java.base", "11.0.8+10", "aix")

	// Invalid architecture
	assertRequestFailure(t, "http://localhost:8080/runtime/a/windows/11.0.8+10", 400)
	// Invalid OS
	assertRequestFailure(t, "http://localhost:8080/runtime/x64/a/11.0.8+10", 400)
	// Invalid version
	assertRequestFailure(t, "http://localhost:8080/runtime/x64/windows/1a3", 400)
	// Nonexistent version
	assertRequestFailure(t, "http://localhost:8080/runtime/x64/windows/99", 400)
	// Invalid module
	assertRequestFailure(t, "http://localhost:8080/runtime/x64/windows/11.0.8+10?modules=123", 400)
	assertRequestFailure(t, "http://localhost:8080/runtime/x64/windows/11.0.8+10?modules=&", 400)

	// Health check
	res, err := http.Get("http://localhost:8080/status")
	assert.NoError(t, err)
	assert.Equal(t, 200, res.StatusCode)
	defer res.Body.Close()
}

func TestVersionRegex(t *testing.T) {
	assert.True(t, versionCheck.MatchString("9"))
	assert.True(t, versionCheck.MatchString("9+1"))
	assert.True(t, versionCheck.MatchString("9.1"))
	assert.True(t, versionCheck.MatchString("9.0.1"))
	assert.True(t, versionCheck.MatchString("9.0.1+11"))
	assert.True(t, versionCheck.MatchString("9.0.1+11.2"))

	assert.False(t, versionCheck.MatchString("9.0.0"))
	assert.False(t, versionCheck.MatchString("9."))
	assert.False(t, versionCheck.MatchString("9+"))
	assert.False(t, versionCheck.MatchString("9.+1"))
	assert.False(t, versionCheck.MatchString(".9"))
	assert.False(t, versionCheck.MatchString("09"))
}

func TestDetermineLocalPlatform(t *testing.T) {
	expectedPlatform := runtime.GOOS
	if expectedPlatform == "darwin" {
		expectedPlatform = "mac"
	}

	assert.Equal(t, "", os.Getenv("LOCAL_PLATFORM"))
	assert.Equal(t, expectedPlatform, determineLocalPlatform())

	os.Setenv("LOCAL_PLATFORM", "darwin")
	assert.Equal(t, "mac", determineLocalPlatform())

	os.Setenv("LOCAL_PLATFORM", "windows")
	assert.Equal(t, "windows", determineLocalPlatform())

	os.Setenv("LOCAL_PLATFORM", "mydiyos")
	assert.Equal(t, "mydiyos", determineLocalPlatform())
}
