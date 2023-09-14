package utils

import (
	"strings"

	"github.com/rs/zerolog"
)

type AnnotationMapping struct {
	PortName string
	Schema   string
	Path     string
}

var validSchema = []string{"http", "https", "https-notlsverify"}

func ParseMapping(log *zerolog.Logger, mapping string) []AnnotationMapping {
	splitted := strings.Split(mapping, ",")
	for i, m := range splitted {
		splitted[i] = strings.TrimSpace(m)
	}
	ret := []AnnotationMapping{}
	for _, m := range splitted {
		splitted := strings.SplitN(m, "/", 3)
		amap := AnnotationMapping{}
		if len(splitted) > 0 && splitted[0] != "" {
			amap.PortName = splitted[0]
		} else {
			log.Warn().Str("splitting", m).Msg("Invalid mapping")
			continue
		}
		if len(splitted) > 1 && splitted[1] != "" {
			for _, s := range validSchema {
				if s == splitted[1] {
					amap.Schema = splitted[1]
				}
			}
			if amap.Schema == "" {
				log.Warn().Str("splitting", m).Msg("Invalid schema")
				continue
			}
		} else {
			amap.Schema = "http"
		}
		if len(splitted) > 2 && splitted[2] != "" {
			amap.Path = "/" + strings.TrimLeft(splitted[2], "/")
		} else {
			amap.Path = "/"
		}
		ret = append(ret, amap)
	}
	return ret
}
