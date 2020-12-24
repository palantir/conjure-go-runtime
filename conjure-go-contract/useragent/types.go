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
	"regexp"
	"strings"

	werror "github.com/palantir/witchcraft-go-error"
)

/*
Conjure format from https://github.com/palantir/conjure/blob/master/docs/spec/wire.md#243-user-agent

User-Agent        = commented-product *( WHITESPACE commented-product )
commented-product = product | product WHITESPACE paren-comments
product           = name "/" version
paren-comments    = "(" comments ")"
comments          = comment-text *( delim comment-text )
delim             = "," | ";"

comment-text      = [^,;()]+
name              = [a-zA-Z][a-zA-Z0-9\-]*
version           = [0-9]+(\.[0-9]+)*(-rc[0-9]+)?(-[0-9]+-g[a-f0-9]+)?
*/

var (
	namePattern    = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9\-]*`)
	versionPattern = regexp.MustCompile(`[0-9]+(\.[0-9]+)*(-rc[0-9]+)?(-[0-9]+-g[a-f0-9]+)?`)
	commentPattern = regexp.MustCompile(`[^,;()]+`)
)

// Product represents a single component in a user-agent string.
type Product struct {
	name     string
	version  string
	comments []string
	fmt.Stringer
}

func NewProduct(name, version string, comments ...string) (Product, error) {
	if !namePattern.MatchString(name) {
		return Product{}, werror.Error("product name is not valid for User-Agent",
			werror.SafeParam("name", name),
			werror.SafeParam("namePattern", namePattern.String()))
	}
	if !versionPattern.MatchString(version) {
		return Product{}, werror.Error("product version is not valid for User-Agent",
			werror.SafeParam("version", version),
			werror.SafeParam("versionPattern", versionPattern.String()))
	}
	for _, comment := range comments {
		if !commentPattern.MatchString(comment) {
			return Product{}, werror.Error("product comment is not valid for User-Agent",
				werror.SafeParam("comment", comment),
				werror.SafeParam("commentPattern", commentPattern.String()))
		}
	}

	return Product{name: name, version: version, comments: comments}, nil
}

func (p *Product) String() string {
	str := strings.Join([]string{p.name, p.version}, "/")
	if len(p.comments) > 0 {
		str = fmt.Sprintf("%s (%s)", str, strings.Join(p.comments, ", "))
	}
	return str
}

// Builder is a container for a list of Product entries that should be rendered into a user-agent.
type Builder struct {
	products []Product
	fmt.Stringer
}

func (b *Builder) Push(product ...Product) {
	b.products = append(b.products, product...)
}

// String produces the full user-agent string to be used in a request header.
// It iterates though the products in LIFO order, so the last product added to the stack is the first in the result.
func (b *Builder) String() string {
	var strs []string
	// iterate in reverse order so last additions are first
	for i := len(b.products) - 1; i >= 0; i-- {
		strs = append(strs, b.products[i].String())
	}
	return strings.Join(strs, " ")
}

func (b *Builder) Clone() *Builder {
	stack := Builder{
		products: make([]Product, len(b.products)),
	}
	copy(stack.products, b.products)
	return &stack
}
