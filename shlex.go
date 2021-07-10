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

/*
Package shlex implements a simple lexer which splits input in to tokens using
shell-style rules for quoting and commenting.

The basic use case uses the default ASCII lexer to split a string into sub-strings:

  shlex.Split("one \"two three\" four") -> []string{"one", "two three", "four"}

To process a stream of strings:

  l := NewLexer(os.Stdin)
  for ; token, err := l.Next(); err != nil {
  	// process token
  }

To access the raw token stream (which includes tokens for comments):

  t := NewTokenizer(os.Stdin)
  for ; token, err := t.Next(); err != nil {
	// process token
  }

*/
package shlex

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// TokenType is a top-level token classification: A word, space, comment, unknown.
type TokenType int8

// runeTokenClass is the type of a UTF-8 character classification: A quote, space, escape.
type runeTokenClass int8

// the internal state used by the lexer state machine
type lexerState int8

// Token is a (type, value) pair representing a lexographical token.
type Token struct {
	tokenType TokenType
	value     string
}

// Equal reports whether tokens a, and b, are equal.
// Two tokens are equal if both their types and values are equal. A nil token can
// never be equal to another token.
func (a *Token) Equal(b *Token) bool {
	if a == nil || b == nil {
		return false
	}
	if a.tokenType != b.tokenType {
		return false
	}
	return a.value == b.value
}

// Named classes of UTF-8 runes
const (
	spaceRunes            = " \t\r\n"
	escapingQuoteRunes    = `"`
	nonEscapingQuoteRunes = "'"
	escapeRunes           = `\`
	commentRunes          = "#"
)

// Classes of rune token
const (
	unknownRuneClass runeTokenClass = iota
	spaceRuneClass
	escapingQuoteRuneClass
	nonEscapingQuoteRuneClass
	escapeRuneClass
	commentRuneClass
	eofRuneClass
)

// Classes of lexographic token
const (
	UnknownToken TokenType = iota
	WordToken
	SpaceToken
	CommentToken
)

// Lexer state machine states
const (
	startState           lexerState = iota // no runes have been seen
	inWordState                            // processing regular runes in a word
	escapingState                          // we have just consumed an escape rune; the next rune is literal
	escapingQuotedState                    // we have just consumed an escape rune within a quoted string
	quotingEscapingState                   // we are within a quoted string that supports escaping ("...")
	quotingState                           // we are within a string that does not support escaping ('...')
	commentState                           // we are within a comment (everything following an unquoted or unescaped #
)

// tokenClassifier is used for classifying rune characters.
type tokenClassifier map[rune]runeTokenClass

func (typeMap tokenClassifier) addRuneClass(runes string, tokenType runeTokenClass) {
	for _, runeChar := range runes {
		typeMap[runeChar] = tokenType
	}
}

// newDefaultClassifier creates a new classifier for ASCII characters.
func newDefaultClassifier() tokenClassifier {
	t := tokenClassifier{}
	t.addRuneClass(spaceRunes, spaceRuneClass)
	t.addRuneClass(escapingQuoteRunes, escapingQuoteRuneClass)
	t.addRuneClass(nonEscapingQuoteRunes, nonEscapingQuoteRuneClass)
	t.addRuneClass(escapeRunes, escapeRuneClass)
	t.addRuneClass(commentRunes, commentRuneClass)
	return t
}

// TODO: use an array
var defaultClassifier = newDefaultClassifier()

// ClassifyRune classifiees a rune
func (t tokenClassifier) ClassifyRune(runeVal rune) runeTokenClass {
	return t[runeVal]
}

var tokenClasses [255]runeTokenClass

func init() {
	for i := range tokenClasses {
		tokenClasses[i] = unknownRuneClass
	}
	set := func(s string, cls runeTokenClass) {
		for _, c := range []byte(s) {
			tokenClasses[c] = cls
		}
	}
	set(spaceRunes, spaceRuneClass)
	set(escapingQuoteRunes, escapingQuoteRuneClass)
	set(nonEscapingQuoteRunes, nonEscapingQuoteRuneClass)
	set(escapeRunes, escapeRuneClass)
	set(commentRunes, commentRuneClass)
}

func classifyRune(r rune) runeTokenClass {
	if r <= 255 {
		return tokenClasses[uint8(r)]
	}
	return unknownRuneClass
}

// Lexer turns an input stream into a sequence of tokens. Whitespace and comments are skipped.
type Lexer Tokenizer

// NewLexer creates a new lexer from an input stream.
func NewLexer(r io.Reader) *Lexer {

	return (*Lexer)(NewTokenizer(r))
}

// Next returns the next word, or an error. If there are no more words,
// the error will be io.EOF.
func (l *Lexer) Next() (string, error) {
	for {
		token, err := (*Tokenizer)(l).Next()
		if err != nil {
			return "", err
		}
		switch token.tokenType {
		case WordToken:
			return token.value, nil
		case CommentToken:
			// skip comments
		default:
			return "", fmt.Errorf("Unknown token type: %v", token.tokenType)
		}
	}
}

// Tokenizer turns an input stream into a sequence of typed tokens
type Tokenizer struct {
	input      bufio.Reader
	classifier tokenClassifier
}

// NewTokenizer creates a new tokenizer from an input stream.
func NewTokenizer(r io.Reader) *Tokenizer {
	input := bufio.NewReader(r)
	classifier := newDefaultClassifier()
	return &Tokenizer{
		input:      *input,
		classifier: classifier,
	}
}

// scanStream scans the stream for the next token using the internal state machine.
// It will panic if it encounters a rune which it does not know how to handle.
func (t *Tokenizer) scanStream() (*Token, error) {
	state := startState
	var tokenType TokenType
	var value []rune
	var nextRune rune
	var nextRuneType runeTokenClass
	var err error

	for {
		nextRune, _, err = t.input.ReadRune()
		nextRuneType = t.classifier.ClassifyRune(nextRune)

		if err != nil {
			if err == io.EOF {
				nextRuneType = eofRuneClass
				err = nil
			} else {
				return nil, err
			}
		}

		switch state {
		case startState: // no runes read yet
			switch nextRuneType {
			case eofRuneClass:
				return nil, io.EOF
			case spaceRuneClass:
				// skip
			case escapingQuoteRuneClass:
				tokenType = WordToken
				state = quotingEscapingState
			case nonEscapingQuoteRuneClass:
				tokenType = WordToken
				state = quotingState
			case escapeRuneClass:
				tokenType = WordToken
				state = escapingState
			case commentRuneClass:
				tokenType = CommentToken
				state = commentState
			default:
				tokenType = WordToken
				value = append(value, nextRune)
				state = inWordState
			}
		case inWordState: // in a regular word
			switch nextRuneType {
			case eofRuneClass:
				token := &Token{
					tokenType: tokenType,
					value:     string(value)}
				return token, err
			case spaceRuneClass:
				token := &Token{
					tokenType: tokenType,
					value:     string(value)}
				return token, err
			case escapingQuoteRuneClass:
				state = quotingEscapingState
			case nonEscapingQuoteRuneClass:
				state = quotingState
			case escapeRuneClass:
				state = escapingState
			default:
				value = append(value, nextRune)
			}
		case escapingState: // the rune after an escape character
			switch nextRuneType {
			case eofRuneClass:
				err = fmt.Errorf("EOF found after escape character")
				token := &Token{
					tokenType: tokenType,
					value:     string(value)}
				return token, err
			default:
				state = inWordState
				value = append(value, nextRune)
			}
		case escapingQuotedState: // the next rune after an escape character, in double quotes
			switch nextRuneType {
			case eofRuneClass:
				err = fmt.Errorf("EOF found after escape character")
				token := &Token{
					tokenType: tokenType,
					value:     string(value)}
				return token, err
			default:
				state = quotingEscapingState
				value = append(value, nextRune)
			}
		case quotingEscapingState: // in escaping double quotes
			switch nextRuneType {
			case eofRuneClass:
				err = fmt.Errorf("EOF found when expecting closing quote")
				token := &Token{
					tokenType: tokenType,
					value:     string(value)}
				return token, err
			case escapingQuoteRuneClass:
				state = inWordState
			case escapeRuneClass:
				state = escapingQuotedState
			default:
				value = append(value, nextRune)
			}
		case quotingState: // in non-escaping single quotes
			switch nextRuneType {
			case eofRuneClass:
				err = fmt.Errorf("EOF found when expecting closing quote")
				token := &Token{
					tokenType: tokenType,
					value:     string(value)}
				return token, err
			case nonEscapingQuoteRuneClass:
				state = inWordState
			default:
				value = append(value, nextRune)
			}
		case commentState: // in a comment
			switch nextRuneType {
			case eofRuneClass:
				token := &Token{
					tokenType: tokenType,
					value:     string(value)}
				return token, err
			case spaceRuneClass:
				if nextRune == '\n' {
					state = startState
					token := &Token{
						tokenType: tokenType,
						value:     string(value)}
					return token, err
				} else {
					value = append(value, nextRune)
				}
			default:
				value = append(value, nextRune)
			}
		default:
			return nil, fmt.Errorf("Unexpected state: %v", state)
		}
	}
}

// Next returns the next token in the stream.
func (t *Tokenizer) Next() (*Token, error) {
	return t.scanStream()
}

type StringTokenizer struct {
	str string
}

// scanStream scans the stream for the next token using the internal state machine.
// It will panic if it encounters a rune which it does not know how to handle.
func (t *StringTokenizer) scanStream() (*Token, error) {
	state := startState
	var tokenType TokenType
	var nextRune rune
	var nextRuneType runeTokenClass
	var err error

	// TODO: grow a reasonable amount
	var value strings.Builder
	// value.Grow(32)

	for {
		if len(t.str) > 0 {
			var n int
			nextRune, n = utf8.DecodeRuneInString(t.str)
			// nextRuneType = classifier.ClassifyRune(nextRune)
			nextRuneType = classifyRune(nextRune)
			t.str = t.str[n:]
		} else {
			nextRuneType = eofRuneClass
		}

		switch state {
		case startState: // no runes read yet
			switch nextRuneType {
			case eofRuneClass:
				return nil, io.EOF
			case spaceRuneClass:
				// skip
			case escapingQuoteRuneClass:
				tokenType = WordToken
				state = quotingEscapingState
			case nonEscapingQuoteRuneClass:
				tokenType = WordToken
				state = quotingState
			case escapeRuneClass:
				tokenType = WordToken
				state = escapingState
			case commentRuneClass:
				tokenType = CommentToken
				state = commentState
			default:
				tokenType = WordToken
				// value = append(value, nextRune)
				value.WriteRune(nextRune)
				state = inWordState
			}
		case inWordState: // in a regular word
			switch nextRuneType {
			case eofRuneClass:
				token := &Token{
					tokenType: tokenType,
					value:     value.String(),
				}
				return token, err
			case spaceRuneClass:
				token := &Token{
					tokenType: tokenType,
					value:     value.String(),
				}
				return token, err
			case escapingQuoteRuneClass:
				state = quotingEscapingState
			case nonEscapingQuoteRuneClass:
				state = quotingState
			case escapeRuneClass:
				state = escapingState
			default:
				// value = append(value, nextRune)
				value.WriteRune(nextRune)
			}
		case escapingState: // the rune after an escape character
			switch nextRuneType {
			case eofRuneClass:
				err = fmt.Errorf("EOF found after escape character")
				token := &Token{
					tokenType: tokenType,
					value:     value.String(),
				}
				return token, err
			default:
				state = inWordState
				// value = append(value, nextRune)
				value.WriteRune(nextRune)
			}
		case escapingQuotedState: // the next rune after an escape character, in double quotes
			switch nextRuneType {
			case eofRuneClass:
				err = fmt.Errorf("EOF found after escape character")
				token := &Token{
					tokenType: tokenType,
					value:     value.String(),
				}
				return token, err
			default:
				state = quotingEscapingState
				// value = append(value, nextRune)
				value.WriteRune(nextRune)
			}
		case quotingEscapingState: // in escaping double quotes
			switch nextRuneType {
			case eofRuneClass:
				err = fmt.Errorf("EOF found when expecting closing quote")
				token := &Token{
					tokenType: tokenType,
					value:     value.String(),
				}
				return token, err
			case escapingQuoteRuneClass:
				state = inWordState
			case escapeRuneClass:
				state = escapingQuotedState
			default:
				// value = append(value, nextRune)
				value.WriteRune(nextRune)
			}
		case quotingState: // in non-escaping single quotes
			switch nextRuneType {
			case eofRuneClass:
				err = fmt.Errorf("EOF found when expecting closing quote")
				token := &Token{
					tokenType: tokenType,
					value:     value.String(),
				}
				return token, err
			case nonEscapingQuoteRuneClass:
				state = inWordState
			default:
				// value = append(value, nextRune)
				value.WriteRune(nextRune)
			}
		case commentState: // in a comment
			switch nextRuneType {
			case eofRuneClass:
				token := &Token{
					tokenType: tokenType,
					value:     value.String(),
				}
				return token, err
			case spaceRuneClass:
				if nextRune == '\n' {
					state = startState
					token := &Token{
						tokenType: tokenType,
						value:     value.String(), // last
					}
					return token, err
				} else {
					// value = append(value, nextRune)
					value.WriteRune(nextRune)
				}
			default:
				// value = append(value, nextRune) // last
				value.WriteRune(nextRune)
			}
		default:
			return nil, fmt.Errorf("Unexpected state: %v", state)
		}
	}
}

// Next returns the next token in the stream.
func (t *StringTokenizer) Next() (*Token, error) {
	return t.scanStream()
}

func (t *StringTokenizer) NextStr() (string, error) {
	for {
		token, err := t.Next()
		if err != nil {
			return "", err
		}
		switch token.tokenType {
		case WordToken:
			return token.value, nil
		case CommentToken:
			// skip comments
		default:
			return "", fmt.Errorf("Unknown token type: %v", token.tokenType)
		}
	}
}

// Split partitions a string into a slice of strings.
func Split(s string) ([]string, error) {
	l := StringTokenizer{str: s}
	// l := NewLexer(strings.NewReader(s))
	subStrings := make([]string, 0)
	for {
		word, err := l.NextStr()
		if err != nil {
			if err == io.EOF {
				return subStrings, nil
			}
			return subStrings, err
		}
		subStrings = append(subStrings, word)
	}
}
