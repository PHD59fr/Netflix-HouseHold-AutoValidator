package main

import (
	"mime"
	"regexp"
)

func extractLinks(text string) []string {
	re := regexp.MustCompile(`https?://\S+`)
	return re.FindAllString(text, -1)
}

func mimeDecoder(encoded string) (string, error) {
	decoder := new(mime.WordDecoder)
	decoded, err := decoder.DecodeHeader(encoded)
	if err != nil {
		return "", err
	}
	return decoded, nil
}
