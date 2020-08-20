/*
Copyright 2020 The Vedette authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

func overrideFields(src, dest *unstructured.Unstructured) {
	recursiveOverride(src.Object, dest.Object)
}

func recursiveOverride(src, dest map[string]interface{}) {
	for k, v := range src {
		switch v.(type) {
		case map[string]interface{}:
			if v == nil || dest[k] == nil {
				dest[k] = v
			} else {
				recursiveOverride(
					v.(map[string]interface{}),
					dest[k].(map[string]interface{}))
			}
		default:
			dest[k] = v
		}
	}
}
