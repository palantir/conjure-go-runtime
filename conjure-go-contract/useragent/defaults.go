// Copyright (c) 2020 Palantir Technologies. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package useragent

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	werror "github.com/palantir/witchcraft-go-error"
)

// Default is a Builder with products representing the versions of golang and conjure-go-runtime.
// Use Push() to add products which apply to all user agents produced by this binary.
// Use Clone() to create a more specific stack whose changes will not affect the default.
var Default = Builder{
	products: []Product{goProduct(), cgrProduct()},
}

func cgrProduct() Product {
	p := Product{name: "conjure-go-runtime"}
	cgr, err := detectDependency("github.com/palantir/conjure-go-runtime/v2")
	if err != nil {
		p.version = "unknown"
	} else {
		p.version = cgr.Version
	}
	return p
}

func goProduct() Product {
	return Product{
		name:     "golang",
		version:  strings.TrimPrefix(runtime.Version(), "go"),
		comments: []string{fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)},
	}
}

func detectDependency(moduleName string) (*debug.Module, error) {
	buildInfo, err := readBuildInfo()
	if err != nil {
		return nil, err
	}
	for _, mod := range buildInfo.Deps {
		if mod.Path == moduleName {
			return mod, nil
		}
	}
	return nil, werror.Error("unable to find module")
}

var (
	buildInfoOnce  = &sync.Once{}
	buildInfoCache *debug.BuildInfo
	buildInfoErr   error
)

func readBuildInfo() (*debug.BuildInfo, error) {
	buildInfoOnce.Do(func() {
		buildInfo, ok := debug.ReadBuildInfo()
		if ok {
			buildInfoCache = buildInfo
		} else {
			buildInfoErr = werror.Error("unable to read runtime/debug build info")
		}
	})
	return buildInfoCache, buildInfoErr
}
