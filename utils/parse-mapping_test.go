package utils

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestParseMapping(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()

	mapping := ParseMapping(&log, "")
	assert.Equal(t, mapping, []AnnotationMapping{})

}

func TestParseMappingPortName(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()

	mapping := ParseMapping(&log, "hallo")
	assert.Equal(t, mapping, []AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
	}})

	mapping = ParseMapping(&log, "//,hallo,/")
	assert.Equal(t, mapping, []AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
	}})

}

func TestParseMappingPortSchema(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()

	mapping := ParseMapping(&log, "hallo/xxxx")
	assert.Equal(t, mapping, []AnnotationMapping{})

	mapping = ParseMapping(&log, "hallo/http")
	assert.Equal(t, mapping, []AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
	}})

	mapping = ParseMapping(&log, "xallo/xxxx/,hallo/http,murks/https")
	assert.Equal(t, mapping, []AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
	},
		{
			PortName: "murks",
			Schema:   "https",
			Path:     "/",
		}})
}

func TestParseMappingPortSchemaPath(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()

	mapping := ParseMapping(&log, "hallo/http/////")
	assert.Equal(t, mapping, []AnnotationMapping{
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/",
		},
	})

	mapping = ParseMapping(&log, "hallo/http/meno/wurks")
	assert.Equal(t, mapping, []AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/meno/wurks",
	}})

	mapping = ParseMapping(&log, "hallo/http/////meno")
	assert.Equal(t, mapping, []AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/meno",
	}})
	mapping = ParseMapping(&log, "hallo/http/////meno/wurks")
	assert.Equal(t, mapping, []AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/meno/wurks",
	}})

	mapping = ParseMapping(&log, `hallo/http/////  ,
		hallo/http/meno ,
		hallo/http/////meno ,
		hallo/http,
		hallo/http/////meno/wurks,hallo/http/meno/wurks/`)
	assert.Equal(t, mapping, []AnnotationMapping{
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/",
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/meno",
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/meno",
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/",
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/meno/wurks",
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/meno/wurks/",
		},
	})
}
