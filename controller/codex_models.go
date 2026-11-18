package controller

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

// 目录来自 OpenAI Codex（Apache-2.0），保留原始模型能力与指令。
// 来源：openai/codex@29091c7234b0e58d4e5474841dd69474d6116ce0
// 文件：codex-rs/models-manager/models.json；许可见 THIRD-PARTY-LICENSES.md。
// 更新时应替换整个快照并运行模型目录测试，不能只按模型名称猜测能力。
//
//go:embed codex_models.json
var codexModelsJSON []byte

type codexCatalogEntry struct {
	slug string
	data json.RawMessage
}

var loadCodexModelCatalog = sync.OnceValues(func() ([]codexCatalogEntry, error) {
	var catalog struct {
		Models []map[string]json.RawMessage `json:"models"`
	}
	if err := common.Unmarshal(codexModelsJSON, &catalog); err != nil {
		return nil, err
	}
	entries := make([]codexCatalogEntry, 0, len(catalog.Models))
	for _, fields := range catalog.Models {
		var slug string
		if err := common.Unmarshal(fields["slug"], &slug); err != nil {
			return nil, err
		}
		var messages struct {
			InstructionsTemplate string `json:"instructions_template"`
		}
		if err := common.Unmarshal(fields["model_messages"], &messages); err != nil {
			return nil, err
		}
		if slug == "" || messages.InstructionsTemplate == "" {
			return nil, fmt.Errorf("invalid Codex catalog entry: %q", slug)
		}
		// 旧版客户端仍要求此字段；新客户端读取 model_messages。
		instructions, err := common.Marshal(messages.InstructionsTemplate)
		if err != nil {
			return nil, err
		}
		fields["base_instructions"] = instructions
		// 目录通过标准 Responses HTTP 接口使用，不能宣告专用后端传输能力。
		fields["prefer_websockets"] = json.RawMessage("false")
		fields["use_responses_lite"] = json.RawMessage("false")
		fields["supports_reasoning_effort_updates"] = json.RawMessage("false")
		data, err := common.Marshal(fields)
		if err != nil {
			return nil, err
		}
		entries = append(entries, codexCatalogEntry{slug: slug, data: data})
	}
	return entries, nil
})

// ListCodexModels 返回 Codex 原生 ModelsResponse，与当前令牌可见模型取交集。
func ListCodexModels(c *gin.Context) {
	modelNames, _, err := getVisibleModelNames(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": i18n.T(c, i18n.MsgDatabaseError), "type": "server_error"}})
		return
	}
	catalog, err := loadCodexModelCatalog()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": i18n.T(c, i18n.MsgModelCatalogUnavailable), "type": "server_error"}})
		return
	}
	visible := make(map[string]bool, len(modelNames))
	for _, name := range modelNames {
		visible[name] = true
	}
	models := make([]json.RawMessage, 0, len(modelNames))
	for _, entry := range catalog {
		if visible[entry.slug] {
			models = append(models, entry.data)
		}
	}
	c.JSON(http.StatusOK, gin.H{"models": models})
}
