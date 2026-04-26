// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package gconv

import "testing"

type mapBenchStruct struct {
	Name        string         `json:"name"`
	Score       int            `json:"score"`
	Age         int            `json:"age"`
	ID          int            `json:"id"`
	Tags        []string       `json:"tags"`
	Extra       map[string]int `json:"extra"`
	Description string         `json:"description"`
}

var (
	mapBenchStringAny = map[string]any{
		"name":        "gf",
		"score":       100,
		"age":         98,
		"id":          199,
		"tags":        []string{"fast", "stable"},
		"extra":       map[string]int{"a": 1, "b": 2},
		"description": "benchmark data",
	}
	mapBenchStringInt = map[string]int{
		"a": 1,
		"b": 2,
		"c": 3,
		"d": 4,
		"e": 5,
		"f": 6,
		"g": 7,
		"h": 8,
	}
	mapBenchStructValue = mapBenchStruct{
		Name:        "gf",
		Score:       100,
		Age:         98,
		ID:          199,
		Tags:        []string{"fast", "stable"},
		Extra:       map[string]int{"a": 1, "b": 2},
		Description: "benchmark data",
	}
	mapBenchJSONString = `{"name":"gf","score":100,"age":98,"id":199,"tags":["fast","stable"],"extra":{"a":1,"b":2},"description":"benchmark data"}`
	mapBenchResult     map[string]any
)

func Benchmark_Map_StringAny_NoDeep(b *testing.B) {
	var result map[string]any
	for i := 0; i < b.N; i++ {
		result = Map(mapBenchStringAny)
	}
	mapBenchResult = result
}

func Benchmark_Map_StringAny_Deep(b *testing.B) {
	var result map[string]any
	for i := 0; i < b.N; i++ {
		result = Map(mapBenchStringAny, MapOption{Deep: true})
	}
	mapBenchResult = result
}

func Benchmark_Map_StringInt(b *testing.B) {
	var result map[string]any
	for i := 0; i < b.N; i++ {
		result = Map(mapBenchStringInt)
	}
	mapBenchResult = result
}

func Benchmark_Map_Struct_Deep(b *testing.B) {
	var result map[string]any
	for i := 0; i < b.N; i++ {
		result = Map(mapBenchStructValue, MapOption{Deep: true})
	}
	mapBenchResult = result
}

func Benchmark_Map_JSONString(b *testing.B) {
	var result map[string]any
	for i := 0; i < b.N; i++ {
		result = Map(mapBenchJSONString)
	}
	mapBenchResult = result
}
