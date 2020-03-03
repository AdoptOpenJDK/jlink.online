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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"

	"github.com/mholt/archiver/v3"
)

type releaseResponse struct {
	Name     string          `json:"release_name" binding:"required"`
	Binaries []releaseBinary `json:"binaries"`
}

type releaseBinary struct {
	FileName       string         `json:"binary_name" binding:"required"`
	Platform       string         `json:"os" binding:"required"`
	Arch           string         `json:"architecture" binding:"required"`
	Implementation string         `json:"openjdk_impl" binding:"required"`
	Link           string         `json:"binary_link" binding:"required"`
	ReleaseVersion releaseVersion `json:"version_data" binding:"required"`
}

type releaseVersion struct {
	Version string `json:"openjdk_version" binding:"required"`
}

var localCache map[string][]releaseBinary

// LoadLocalReleaseCache loads the local cache from the included JSON file.
func loadLocalReleaseCache() error {
	releases, err := os.Open("adoptopenjdk.json")
	if err != nil {
		return err
	}

	bytes, err := ioutil.ReadAll(releases)
	if err != nil {
		return err
	}

	json.Unmarshal(bytes, &localCache)
	return nil
}

// LookupRelease finds a release for the given version string.
func lookupRelease(arch, platform, implementation, version string) (*releaseBinary, error) {

	// Search local cache first
	for v, releases := range localCache {
		if version == strings.Split(v, "+")[0] {
			for _, release := range releases {
				if release.Platform == platform && release.Arch == arch && release.Implementation == implementation {
					return &release, nil
				}
			}
		}
	}

	majorVersion, _ := getMajorVersion(version)

	res, err := adoptOpenJdk.Get(fmt.Sprintf("https://api.adoptopenjdk.net/v2/info/releases/openjdk%d?openjdk_impl=%s&os=%s&arch=%s&type=jdk", majorVersion, implementation, platform, arch))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var data []releaseResponse
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	for _, release := range data {
		if i := strings.Index(release.Name, "+"); i != -1 {
			if release.Name[4:i] == version {
				for _, binary := range release.Binaries {
					if binary.Platform == platform && binary.Arch == arch {
						// Cache result
						if localCache[binary.ReleaseVersion.Version] == nil {
							localCache[binary.ReleaseVersion.Version] = make([]releaseBinary, 0)
						}
						localCache[binary.ReleaseVersion.Version] = append(localCache[binary.ReleaseVersion.Version], binary)

						return &binary, nil
					}
				}
			}
		}
	}

	return nil, nil
}

// LookupLatestRelease finds the latest release of the given major version.
func lookupLatestRelease(arch, platform, implementation string, majorVersion int) (*releaseBinary, error) {
	res, err := adoptOpenJdk.Get(fmt.Sprintf("https://api.adoptopenjdk.net/v2/info/releases/openjdk%d?openjdk_impl=%s&os=%s&arch=%s&type=jdk&release=latest", majorVersion, implementation, platform, arch))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var data releaseResponse
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	for _, binary := range data.Binaries {
		if binary.Platform == platform && binary.Arch == arch {
			return &binary, nil
		}
	}

	return nil, nil
}

// Only one runtime can be downloaded at a time. This is to prevent issues with
// partial downloads.
var downloadLock sync.Mutex

// DownloadRelease downloads a JDK runtime image from AdoptOpenJDK.
func downloadRelease(release *releaseBinary) (string, error) {
	downloadLock.Lock()
	defer downloadLock.Unlock()

	output := RT_CACHE + "/" + strings.TrimSuffix(strings.TrimSuffix(release.FileName, ".zip"), ".tar.gz")

	// Check if the runtime is cached
	if _, e := os.Stat(output); !os.IsNotExist(e) {
		return output + "/jdk-" + release.ReleaseVersion.Version, nil
	}

	archive, dir := newTemporaryFile(release.FileName)
	defer os.RemoveAll(dir)

	// Download the runtime
	if err := download(github, release.Link, archive); err != nil {
		return "", err
	}

	// Extract to the cache directory
	if err := archiver.Unarchive(archive, output); err != nil {
		return "", err
	}

	return output + "/jdk-" + release.ReleaseVersion.Version, nil
}

// UpdateLocalReleaseCache redownloads release metadata from AdoptOpenJDK.
func updateLocalReleaseCache() error {
	cache := make(map[string][]releaseBinary)

	// Repopulate local cache
	for _, majorVersion := range []string{"9", "10", "11", "12", "13"} {
		res, err := adoptOpenJdk.Get(fmt.Sprintf("https://api.adoptopenjdk.net/v2/info/releases/openjdk%s?type=jdk", majorVersion))
		if err != nil {
			return err
		}
		defer res.Body.Close()

		var data []releaseResponse
		if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
			fmt.Println(err)
		}

		for _, release := range data {
			for _, binary := range release.Binaries {
				if cache[binary.ReleaseVersion.Version] == nil {
					cache[binary.ReleaseVersion.Version] = make([]releaseBinary, 0)
				}
				cache[binary.ReleaseVersion.Version] = append(cache[binary.ReleaseVersion.Version], binary)
			}
		}
	}

	// Prune outdated releases
	/*for k1 := range cache {
		for k2 := range cache {
			v1 := strings.Split(k1, "+")[0]
			v2 := strings.Split(k2, "+")[0]
			r1 := strings.Split(k1, "+")[1]
			r2 := strings.Split(k2, "+")[1]
			if v1 == v2 && r1 != r2 {
				x1 := strings.Split(r1, ".")
				x2 := strings.Split(r2, ".")

				// Normalize array lengths
				for i := 0; i < len(x2) - len(x1); i++ {
					x1 = append(x1, "0")
				}
				for i := 0; i < len(x1) - len(x2); i++ {
					x2 = append(x2, "0")
				}

				// Perform comparison
				for i, _ := range x1 {
					y1, _ := strconv.Atoi(x1[i])
					y2, _ := strconv.Atoi(x2[i])

					if y1 > y2 {
						delete(cache.Releases, k2)
						break
					} else if y1 < y2 {
						delete(cache.Releases, k1)
						break
					}
				}
			}
		}
	}*/

	data, err := json.MarshalIndent(&cache, "", " ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile("adoptopenjdk.json", data, 0644)
	if err != nil {
		return err
	}

	localCache = cache
	return nil
}
