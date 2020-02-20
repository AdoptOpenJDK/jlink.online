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
	"strings"
)

type releaseResponse struct {
	Name     string          `json:"release_name" binding:"required"`
	Binaries []releaseBinary `json:"binaries"`
}

type releaseBinary struct {
	FileName       string         `json:"binary_name" binding:"required"`
	Platform       string         `json:"os" binding:"required"`
	Arch           string         `json:"architecture" binding:"required"`
	Link           string         `json:"binary_link" binding:"required"`
	ReleaseVersion releaseVersion `json:"version_data" binding:"required"`
}

type releaseVersion struct {
	Version string `json:"openjdk_version" binding:"required"`
}

// LookupRelease finds a release for the given version string.
func lookupRelease(arch, platform, implementation, version string) (*releaseBinary, error) {
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
