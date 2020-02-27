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
	"errors"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

func download(client *http.Client, source, dest string) error {
	log.Println("Downloading:", source)

	res, err := client.Get(source)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return errors.New("Status Code: " + res.Status)
	}
	defer res.Body.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, res.Body)
	if err != nil {
		return err
	}

	return nil
}

func downloadBytes(client *http.Client, source string) ([]byte, error) {
	log.Println("Downloading:", source)

	res, err := client.Get(source)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("Status Code: " + res.Status)
	}
	defer res.Body.Close()

	b := new(bytes.Buffer)
	b.ReadFrom(res.Body)
	return b.Bytes(), nil
}

// GetMajorVersion returns the major version field from a Java version string.
func getMajorVersion(version string) (int, error) {
	if i := strings.Index(version, "."); i != -1 {
		version = version[:i]
	}

	return strconv.Atoi(version)
}

var parseModules = regexp.MustCompile(`requires[\s]*(transitive)?[\s]+([\w\.]+)[\s]*;`)

// ParseModuleInfo extracts the module dependencies from a module-info.java file.
func parseModuleInfo(file string) []string {
	var modules []string
	for _, match := range parseModules.FindAllStringSubmatch(file, -1) {
		modules = append(modules, match[2])
	}

	return modules
}

// NewTemporaryFile returns a new temporary file and its parent directory.
func newTemporaryFile(filename string) (string, string) {
	dir := TMP + "/" + strconv.Itoa(rand.Int())
	_ = os.MkdirAll(dir, os.ModePerm)
	return dir + "/" + filename, dir
}

// NewTemporaryDirectory returns a new temporary directory and its parent directory.
func newTemporaryDirectory(dirname string) (string, string) {
	dir := TMP + "/" + strconv.Itoa(rand.Int())
	_ = os.MkdirAll(dir+"/"+dirname, os.ModePerm)
	return dir + "/" + dirname, dir
}
