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
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"strings"
)

type pom struct {
	Dependencies []dependency `xml:"dependencies>dependency"`
}

type dependency struct {
	GroupId    string `xml:"groupId"`
	ArtifactId string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
}

// DownloadArtifacts downloads artifacts and their dependencies from Maven Central.
func downloadArtifacts(output string, artifacts []string) error {
	for _, artifact := range artifacts {
		gav := strings.Split(artifact, ":")
		if len(gav) != 3 {
			return errors.New("Invalid maven coordinates: " + artifact)
		}

		// Check if the artifact already exists
		if _, e := os.Stat(fmt.Sprintf("%s/%s-%s.jar", output, gav[1], gav[2])); !os.IsNotExist(e) {
			continue
		}

		base := fmt.Sprintf("https://repo1.maven.org/maven2/%s/%s/%s", strings.ReplaceAll(gav[0], ".", "/"), gav[1], gav[2])
		err := download(mavenCentral, fmt.Sprintf("%s/%s-%s.jar", base, gav[1], gav[2]), fmt.Sprintf("%s/%s-%s.jar", output, gav[1], gav[2]))
		if err != nil {
			return err
		}
		data, err := downloadBytes(mavenCentral, fmt.Sprintf("%s/%s-%s.pom", base, gav[1], gav[2]))
		if err != nil {
			return err
		}

		var artifactPom pom
		if err := xml.Unmarshal(data, &artifactPom); err != nil {
			return err
		}

		var depArtifacts []string
		for _, dep := range artifactPom.Dependencies {
			// Skip test dependencies
			if dep.Scope == "test" {
				continue
			}
			depArtifacts = append(depArtifacts, fmt.Sprintf("%s:%s:%s", dep.GroupId, dep.ArtifactId, dep.Version))
		}

		if err := downloadArtifacts(output, depArtifacts); err != nil {
			return err
		}
	}

	return nil
}
