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
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mholt/archiver/v3"
	"github.com/stretchr/testify/assert"
)

func assertRequestSuccess(t *testing.T, req, platform string) {
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

	files, err := ioutil.ReadDir(output)
	assert.NoError(t, err)
	for _, f := range files {
		switch platform {
		case "windows":
			_, err := os.Stat(filepath.FromSlash(output + "/" + f.Name() + "/bin/java.exe"))
			assert.NoError(t, err)
		default:
			_, err := os.Stat(filepath.FromSlash(output + "/" + f.Name() + "/bin/java"))
			assert.NoError(t, err)
		}
	}

	// Execute "java --version" according to the test platform
	if LOCAL_PLATFORM == platform {
		log.Println("Executing 'java --version' on local platform")
		for _, f := range files {
			switch platform {
			case "windows":
				cmd := exec.Command(filepath.FromSlash(output+"/"+f.Name()+"/bin/java.exe"), "--version")
				assert.NoError(t, cmd.Run())
			default:
				cmd := exec.Command(filepath.FromSlash(output+"/"+f.Name()+"/bin/java"), "--version")
				assert.NoError(t, cmd.Run())
			}
		}
	}
}

func assertRequestFailure(t *testing.T, req string, expectedCode int) {
	res, err := http.Get(req)

	assert.NoError(t, err)
	assert.Equal(t, expectedCode, res.StatusCode)
}

func TestJlink(t *testing.T) {
	os.Setenv("PORT", "8080")
	go main()

	// Allow the server some time to start
	time.Sleep(4 * time.Second)

	assertRequestSuccess(t, "http://localhost:8080/x64/linux/11.0.8+10?modules=java.base", "linux")
	assertRequestSuccess(t, "http://localhost:8080/x64/windows/11.0.8+10?modules=java.base", "windows")
	assertRequestSuccess(t, "http://localhost:8080/x64/mac/11.0.8+10?modules=java.base", "mac")
	assertRequestSuccess(t, "http://localhost:8080/ppc64/aix/11.0.8+10?modules=java.base", "aix")

	// Invalid architecture
	assertRequestFailure(t, "http://localhost:8080/a/windows/11.0.8+10", 400)
	// Invalid OS
	assertRequestFailure(t, "http://localhost:8080/x64/a/11.0.8+10", 400)
	// Invalid version
	assertRequestFailure(t, "http://localhost:8080/x64/windows/1a3", 400)
	// Nonexistent version
	assertRequestFailure(t, "http://localhost:8080/x64/windows/99", 400)
	// Invalid module
	assertRequestFailure(t, "http://localhost:8080/x64/windows/11.0.8+10?modules=123", 400)
	assertRequestFailure(t, "http://localhost:8080/x64/windows/11.0.8+10?modules=&", 400)
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
