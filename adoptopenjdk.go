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

// A cache to reduce lookups to https://api.adoptopenjdk.net.
var metadataCache map[string][]releaseBinary

// Only one thread can read from or write to the local cache at a time.
var metadataCacheLock sync.Mutex

// Only one runtime can be downloaded at a time. This is to prevent issues with
// partial downloads.
var downloadLock sync.Mutex

// CacheRelease inserts the given release into the local cache.
func cacheRelease(release releaseBinary) {
	version := strings.Split(release.ReleaseVersion.Version, "+")[0]
	if metadataCache[version] == nil {
		metadataCache[version] = make([]releaseBinary, 0)
	}

	for i, r := range metadataCache[version] {
		if release.Platform == r.Platform && release.Arch == r.Arch && release.Implementation == r.Implementation {
			if compareJdkRelease(r.ReleaseVersion.Version, release.ReleaseVersion.Version) < 0 {
				metadataCache[version][i] = release
			}
			return
		}
	}

	metadataCache[version] = append(metadataCache[version], release)
}

// LoadLocalReleaseCache loads the local cache from the included JSON file.
func loadLocalReleaseCache() error {
	metadataCacheLock.Lock()
	defer metadataCacheLock.Unlock()

	releases, err := os.Open("adoptopenjdk.json")
	if err != nil {
		return err
	}

	bytes, err := ioutil.ReadAll(releases)
	if err != nil {
		return err
	}

	json.Unmarshal(bytes, &metadataCache)
	return nil
}

// LookupRelease finds a release for the given version string.
func lookupRelease(arch, platform, implementation, version string) (*releaseBinary, error) {
	metadataCacheLock.Lock()
	defer metadataCacheLock.Unlock()

	// Search local cache first
	if releases := metadataCache[version]; releases != nil {
		for _, release := range releases {
			if release.Platform == platform && release.Arch == arch && release.Implementation == implementation {
				return &release, nil
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
						cacheRelease(binary)
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
	metadataCacheLock.Lock()
	defer metadataCacheLock.Unlock()

	metadataCache = make(map[string][]releaseBinary)

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
				// Filter "testimage"
				if binary.Implementation == "testimage" {
					continue
				}

				cacheRelease(binary)
			}
		}
	}

	data, err := json.MarshalIndent(&metadataCache, "", " ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile("adoptopenjdk.json", data, 0644)
	if err != nil {
		return err
	}

	return nil
}
