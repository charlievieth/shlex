/*
Copyright 2012 Google Inc. All Rights Reserved.

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

package shlex

import (
	"encoding/json"
	"io/ioutil"
	"reflect"
	"strings"
	"testing"
)

var (
	// one two "three four" "five \"six\"" seven#eight # nine # ten
	// eleven 'twelve\'
	testString = "one two \"three four\" \"five \\\"six\\\"\" seven#eight # nine # ten\n eleven 'twelve\\' thirteen=13 fourteen/14"
)

func TestClassifier(t *testing.T) {
	classifier := newDefaultClassifier()
	tests := map[rune]runeTokenClass{
		' ':  spaceRuneClass,
		'"':  escapingQuoteRuneClass,
		'\'': nonEscapingQuoteRuneClass,
		'#':  commentRuneClass}
	for runeChar, want := range tests {
		got := classifier.ClassifyRune(runeChar)
		if got != want {
			t.Errorf("ClassifyRune(%v) -> %v. Want: %v", runeChar, got, want)
		}
	}
}

func TestTokenizer(t *testing.T) {
	testInput := strings.NewReader(testString)
	expectedTokens := []*Token{
		{WordToken, "one"},
		{WordToken, "two"},
		{WordToken, "three four"},
		{WordToken, "five \"six\""},
		{WordToken, "seven#eight"},
		{CommentToken, " nine # ten"},
		{WordToken, "eleven"},
		{WordToken, "twelve\\"},
		{WordToken, "thirteen=13"},
		{WordToken, "fourteen/14"}}

	tokenizer := NewTokenizer(testInput)
	for i, want := range expectedTokens {
		got, err := tokenizer.Next()
		if err != nil {
			t.Error(err)
		}
		if !got.Equal(want) {
			t.Errorf("Tokenizer.Next()[%v] of %q -> %v. Want: %v", i, testString, got, want)
		}
	}
}

func TestLexer(t *testing.T) {
	testInput := strings.NewReader(testString)
	expectedStrings := []string{"one", "two", "three four", "five \"six\"", "seven#eight", "eleven", "twelve\\", "thirteen=13", "fourteen/14"}

	lexer := NewLexer(testInput)
	for i, want := range expectedStrings {
		got, err := lexer.Next()
		if err != nil {
			t.Error(err)
		}
		if got != want {
			t.Errorf("Lexer.Next()[%v] of %q -> %v. Want: %v", i, testString, got, want)
		}
	}
}

func TestSplit(t *testing.T) {
	want := []string{"one", "two", "three four", "five \"six\"", "seven#eight", "eleven", "twelve\\", "thirteen=13", "fourteen/14"}
	got, err := Split(testString)
	if err != nil {
		t.Error(err)
	}
	if len(want) != len(got) {
		t.Errorf("Split(%q) -> %v. Want: %v", testString, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Split(%q)[%v] -> %v. Want: %v", testString, i, got[i], want[i])
		}
	}
}

func BenchmarkSplit(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Split(testString)
	}
}

type TestCase struct {
	Input  string   `json:"input"`
	Output []string `json:"output"`
}

func LoadTestCase(t *testing.T, name string) []TestCase {
	data, err := ioutil.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var tests []TestCase
	if err := json.Unmarshal(data, &tests); err != nil {
		t.Fatal(err)
	}
	return tests
}

// TODO: make this pass (or just ignore it)
func TestPythonCompat(t *testing.T) {
	t.Skip("FIXME")

	t.Run("Shlex", func(t *testing.T) {
		tests := LoadTestCase(t, "testdata/data.json")
		for _, x := range tests {
			got, err := Split(x.Input)
			if err != nil {
				t.Errorf("%q: error: %v", x.Input, err)
				continue
			}
			if !reflect.DeepEqual(got, x.Output) {
				t.Errorf("%q: got: %q want: %q", x.Input, got, x.Output)
			}
		}
	})
	t.Run("Posix", func(t *testing.T) {
		tests := LoadTestCase(t, "testdata/posix_data.json")
		for _, x := range tests {
			got, err := Split(x.Input)
			if err != nil {
				t.Errorf("%q: error: %v", x.Input, err)
				continue
			}
			if !reflect.DeepEqual(got, x.Output) {
				t.Errorf("%q: got: %q want: %q", x.Input, got, x.Output)
			}
		}
	})
}
