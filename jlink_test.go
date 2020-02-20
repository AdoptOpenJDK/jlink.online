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
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func assertRequestSuccess(t *testing.T, req string, expectedCode, expectedPayloadSize int) {
	res, err := http.Get(req)

	assert.NoError(t, err)
	assert.Equal(t, expectedCode, res.StatusCode)

	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	assert.NoError(t, err)

	// Just ensure it's approximately the right size in bytes for now
	// TODO extract and sanity check contents
	assert.True(t, expectedPayloadSize-len(body) <= 10000 && expectedPayloadSize-len(body) >= -10000)
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

	assertRequestSuccess(t, "http://localhost:8080/x64/linux/13?modules=java.base", 200, 18500662)
	assertRequestSuccess(t, "http://localhost:8080/x64/windows/13?modules=java.base", 200, 17831320)
	assertRequestFailure(t, "http://localhost:8080/x64/windows/1a3?modules=java.base", 400)
}
