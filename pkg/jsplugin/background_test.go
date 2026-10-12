package jsplugin

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackgroundSubmissionManifest(t *testing.T) {
	const source = `export const meta={apiVersion:1,key:"background",name:"后台",version:"1.0.0",author:{name:"Test"},models:["image"],fetchMode:"per_task",submitMode:"background"};
 export function buildSubmitRequest(){} export function parseSubmitResponse(){} export function buildQueryRequest(){} export function parseTaskResult(){}`
	plugin, err := CompilePlugin(source, Options{})
	require.NoError(t, err)
	assert.Equal(t, "background", plugin.Meta.SubmitMode)
	for _, replacement := range []string{`submitMode:"unknown"`, `submitMode:true`, `submitMode:"background",protocols:["openai_image"]`, `submitMode:"background",routes:[{method:"POST",path:"/image",type:"submit",decode:"request",render:"response"}]`} {
		_, err := CompilePlugin(strings.Replace(source, `submitMode:"background"`, replacement, 1), Options{})
		require.Error(t, err)
	}
}

func TestBackgroundJSONStateLargeImageBudget(t *testing.T) {
	payload := strings.Repeat("x", 2<<20)
	changes := []any{map[string]any{"op": "set", "path": []any{}, "value": payload}}
	state := NewJSONState(MaxSubmitResultBytes)
	require.NoError(t, state.Apply(t.Context(), changes))
	value, err := state.Value()
	require.NoError(t, err)
	assert.Equal(t, payload, value)
	require.Error(t, NewJSONState(MaxJSONToolBytes).Apply(t.Context(), changes))
	require.Error(t, NewJSONState(len(payload)).Apply(t.Context(), changes), "JSON 引号也计入上限")
}
