package utils

import (
	"os"
	"testing"

	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestParseMapping(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()

	mapping := ParseMapping(&log, "")
	assert.Equal(t, mapping, []types.AnnotationMapping{})

}

func TestParseMappingPortName(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()

	mapping := ParseMapping(&log, "hallo")
	assert.Equal(t, mapping, []types.AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
		Order:    0,
	}})

	mapping = ParseMapping(&log, "//,hallo,/")
	assert.Equal(t, mapping, []types.AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
		Order:    0,
	}})

}

func TestParseMappingPortSchema(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()

	mapping := ParseMapping(&log, "hallo/xxxx")
	assert.Equal(t, mapping, []types.AnnotationMapping{})

	mapping = ParseMapping(&log, "hallo/http")
	assert.Equal(t, mapping, []types.AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
		Order:    0,
	}})

	mapping = ParseMapping(&log, "xallo/xxxx/,hallo/http,murks/https")
	assert.Equal(t, mapping, []types.AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
		Order:    0,
	},
		{
			PortName: "murks",
			Schema:   "https",
			Path:     "/",
			Order:    1,
		},
	})
}

func TestParseMappingNextJS(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()
	mapping := ParseMapping(&log, "public/http|^\\/(assets|favicon|fonts|logo)\\/,public/http|^\\/manifest.json$,next/http/")
	assert.Equal(t, mapping, []types.AnnotationMapping{
		{
			PortName: "public",
			Schema:   "http",
			Path:     "^\\/(assets|favicon|fonts|logo)\\/",
			Order:    0,
		},
		{
			PortName: "public",
			Schema:   "http",
			Path:     "^\\/manifest.json$",
			Order:    1,
		},
		{
			PortName: "next",
			Schema:   "http",
			Path:     "/",
			Order:    2,
		},
	})
}

func TestParseMappingPortSchemaPath(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()
	mapping := ParseMapping(&log, "hallo/http/////")
	assert.Equal(t, mapping, []types.AnnotationMapping{
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/",
			Order:    0,
		},
	})

	mapping = ParseMapping(&log, "hallo/http|||||||")
	assert.Equal(t, mapping, []types.AnnotationMapping{
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "||||||",
			Order:    0,
		},
	})
	mapping = ParseMapping(&log, "hallo/http|")
	assert.Equal(t, mapping, []types.AnnotationMapping{
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "",
			Order:    0,
		},
	})

	mapping = ParseMapping(&log, "hallo/http|meno/wurks")
	assert.Equal(t, mapping, []types.AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "meno/wurks",
		Order:    0,
	}})

	mapping = ParseMapping(&log, "hallo/http|||||||/////meno")
	assert.Equal(t, mapping, []types.AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "||||||/////meno",
		Order:    0,
	}})
	mapping = ParseMapping(&log, "hallo/http||||/////meno/wurks")
	assert.Equal(t, mapping, []types.AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "|||/////meno/wurks",
		Order:    0,
	}})

	mapping = ParseMapping(&log, "hallo/http/meno/wurks")
	assert.Equal(t, mapping, []types.AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/meno/wurks",
		Order:    0,
	}})

	mapping = ParseMapping(&log, "hallo/http/////meno")
	assert.Equal(t, mapping, []types.AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/meno",
		Order:    0,
	}})
	mapping = ParseMapping(&log, "hallo/http/////meno/wurks")
	assert.Equal(t, mapping, []types.AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/meno/wurks",
		Order:    0,
	}})

	mapping = ParseMapping(&log, "hallo/http")
	assert.Equal(t, mapping, []types.AnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
		Order:    0,
	}})

	mapping = ParseMapping(&log, `hallo/http/////  ,
		hallo/http|||/me/no ,
		hallo/http/meno ,
		hallo/http/////meno ,
		hallo/http,
		hallo/http/////meno/wurks,hallo/http/meno/wurks/`)
	assert.Equal(t, mapping, []types.AnnotationMapping{
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/",
			Order:    0,
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "||/me/no",
			Order:    1,
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/meno",
			Order:    2,
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/meno",
			Order:    3,
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/",
			Order:    4,
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/meno/wurks",
			Order:    5,
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/meno/wurks/",
			Order:    6,
		},
	})
}
