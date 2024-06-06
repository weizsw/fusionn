package repository

import astisub "github.com/asticode/go-astisub"

type IParser interface {
	Parse(input string) (*astisub.Subtitles, error)
}

type parser struct{}

func NewParser() *parser {
	return &parser{}
}

func (p *parser) Parse(input string) (*astisub.Subtitles, error) {
	s, err := astisub.OpenFile(input)
	if err != nil {
		return nil, err
	}
	return s, nil
}
