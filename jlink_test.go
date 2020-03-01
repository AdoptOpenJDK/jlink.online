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
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/mholt/archiver/v3"
	"github.com/stretchr/testify/assert"
)

func assertRequestSuccess(t *testing.T, req, filetype string) {
	res, err := http.Get(req)

	assert.NoError(t, err)
	assert.Equal(t, 200, res.StatusCode)
	defer res.Body.Close()

	archive, dir := newTemporaryFile("jdk" + filetype)
	defer os.RemoveAll(dir)

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
		if filetype == ".zip" {
			_, err := os.Stat(output + "/" + f.Name() + "/bin/java.exe")
			assert.NoError(t, err)
		} else {
			_, err := os.Stat(output + "/" + f.Name() + "/bin/java")
			assert.NoError(t, err)
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
	os.Setenv("CACHE", "/tmp/cache")
	go main()

	// Allow the server some time to start
	time.Sleep(4 * time.Second)

	assertRequestSuccess(t, "http://localhost:8080/x64/linux/13?modules=java.base", ".tar.gz")
	assertRequestSuccess(t, "http://localhost:8080/x64/windows/13?modules=java.base", ".zip")
	assertRequestSuccess(t, "http://localhost:8080/x64/mac/13?modules=java.base", ".tar.gz")

	// Invalid architecture
	assertRequestFailure(t, "http://localhost:8080/a/windows/13", 400)
	// Invalid OS
	assertRequestFailure(t, "http://localhost:8080/x64/a/13", 400)
	// Invalid version
	assertRequestFailure(t, "http://localhost:8080/x64/windows/1a3", 400)
	// Nonexistent version
	assertRequestFailure(t, "http://localhost:8080/x64/windows/99", 400)
	// Invalid module
	assertRequestFailure(t, "http://localhost:8080/x64/windows/13?modules=123", 400)
	assertRequestFailure(t, "http://localhost:8080/x64/windows/13?modules=&", 400)
}
