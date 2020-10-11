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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetMajorVersion(t *testing.T) {
	m, err := getMajorVersion("9")
	assert.Equal(t, 9, m)
	assert.NoError(t, err)

	m, err = getMajorVersion("9.1")
	assert.Equal(t, 9, m)
	assert.NoError(t, err)

	m, err = getMajorVersion("9+1")
	assert.Equal(t, 9, m)
	assert.NoError(t, err)

	m, err = getMajorVersion("9.0.1")
	assert.Equal(t, 9, m)
	assert.NoError(t, err)

	m, err = getMajorVersion("9a.1")
	assert.Error(t, err)
}

func TestParseModuleInfo(t *testing.T) {
	assert.Equal(t, []string{"org.slf4j"}, parseModuleInfo(`
		module com.abc {
			exports com.abc;

			requires org.slf4j;
		}
	`))

	assert.Equal(t, []string{"org.slf4j", "api"}, parseModuleInfo(`
		module com.abc {
			exports com.abc ;

			requires
			transitive org.slf4j ;

			requires  api  ;
		}
	`))
}
