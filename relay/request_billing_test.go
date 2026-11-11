package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type inputLimitRequest struct {
	dto.BaseRequest
	tokens int
}

func (r *inputLimitRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{TokenType: types.TokenTypeTextNumber, CombineText: strings.Repeat("a", r.tokens)}
}

func TestPrepareRequestBillingModelInputLimit(t *testing.T) {
	previousConfig := config.GlobalConfig.ExportAllConfigs()
	previousCount := constant.CountToken
	t.Cleanup(func() {
		constant.CountToken = previousCount
		require.NoError(t, config.GlobalConfig.LoadFromDB(previousConfig))
	})
	// A free fixture group avoids any database or wallet access for allowed requests.
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"group_ratio_setting.group_ratio":             `{"default":0}`,
		"quota_setting.enable_free_model_pre_consume": "false",
	}))
	cases := []struct {
		name      string
		model     string
		tokens    int
		count     bool
		wantError bool
	}{
		{"below limit", "kimi-k3.1-pro", 999999, true, false},
		{"exact limit", "kimi-k3.1-pro", 1000000, true, false},
		{"above limit", "kimi-k3.1-pro", 1000001, true, true},
		{"counting disabled still rejects", "kimi-k3.1-pro", 1000001, false, true},
		{"counting disabled retains billing estimate", "kimi-k3.1-pro", 999999, false, false},
		{"highspeed unchanged", "kimi-k3.1-pro-highspeed", 1000001, true, false},
		{"other model unchanged", "gpt-4o", 1000001, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			constant.CountToken = tc.count
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			common.SetContextKey(ctx, constant.ContextKeyOriginalModel, tc.model)
			info := &relaycommon.RelayInfo{OriginModelName: tc.model, UserGroup: "default", UsingGroup: "default",
				RelayFormat: types.RelayFormatClaude, Request: &inputLimitRequest{tokens: tc.tokens}}
			info.UserSetting.AcceptUnsetRatioModel = true
			apiErr := PrepareRequestBilling(ctx, info)
			if tc.wantError {
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
				assert.Equal(t, "Prompt exceeds max length", apiErr.Error())
				assert.Equal(t, types.ErrorCode("context_length_exceeded"), apiErr.GetErrorCode())
				assert.True(t, types.IsSkipRetryError(apiErr))
			} else {
				require.Nil(t, apiErr)
				if tc.count {
					assert.Equal(t, tc.tokens, info.GetEstimatePromptTokens())
				} else {
					assert.Zero(t, info.GetEstimatePromptTokens())
				}
			}
			assert.Nil(t, info.Billing)
		})
	}
}
