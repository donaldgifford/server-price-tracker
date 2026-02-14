package extract_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	extractMocks "github.com/donaldgifford/server-price-tracker/pkg/extract/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestLLMExtractor_Classify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		title      string
		setupMock  func(*extractMocks.MockLLMBackend)
		wantType   domain.ComponentType
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:  "classifies as ram",
			title: "Samsung 32GB DDR4 ECC REG",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.Anything).
					Return(extract.GenerateResponse{Content: "ram"}, nil).
					Once()
			},
			wantType: domain.ComponentRAM,
		},
		{
			name:  "classifies as cpu with whitespace",
			title: "Intel Xeon Gold 6130",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.Anything).
					Return(extract.GenerateResponse{Content: "  CPU  \n"}, nil).
					Once()
			},
			wantType: domain.ComponentCPU,
		},
		{
			name:  "classifies as other",
			title: "Random accessory",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.Anything).
					Return(extract.GenerateResponse{Content: "other"}, nil).
					Once()
			},
			wantType: domain.ComponentOther,
		},
		{
			name:  "invalid type from LLM",
			title: "Something",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.Anything).
					Return(extract.GenerateResponse{Content: "gpu"}, nil).
					Once()
			},
			wantErr:    true,
			wantErrMsg: "invalid component type",
		},
		{
			name:  "LLM backend error",
			title: "Test",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.Anything).
					Return(extract.GenerateResponse{}, errors.New("timeout")).
					Once()
			},
			wantErr:    true,
			wantErrMsg: "calling LLM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockBackend := extractMocks.NewMockLLMBackend(t)
			tt.setupMock(mockBackend)

			extractor := extract.NewLLMExtractor(mockBackend)
			ct, err := extractor.Classify(context.Background(), tt.title)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantType, ct)
		})
	}
}

func TestLLMExtractor_Classify_NonLatinTitle(t *testing.T) {
	t.Parallel()

	// Edge case: Title in non-Latin script should classify as "other".
	mockBackend := extractMocks.NewMockLLMBackend(t)
	mockBackend.EXPECT().
		Generate(mock.Anything, mock.Anything).
		Return(extract.GenerateResponse{Content: "other"}, nil).
		Once()

	extractor := extract.NewLLMExtractor(mockBackend)
	ct, err := extractor.Classify(context.Background(), "三星 32GB DDR4 内存条 服务器 ECC")
	require.NoError(t, err)
	assert.Equal(t, domain.ComponentOther, ct)
}

func TestLLMExtractor_Extract_LOTTitle(t *testing.T) {
	t.Parallel()

	// Edge case: Title says "LOT OF 4" but eBay quantity=1.
	// LLM extraction should override with quantity=4.
	mockBackend := extractMocks.NewMockLLMBackend(t)
	mockBackend.EXPECT().
		Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
			return r.Format == "json"
		})).
		Return(extract.GenerateResponse{
			Content: `{
				"manufacturer": "Samsung",
				"capacity_gb": 32,
				"generation": "DDR4",
				"speed_mhz": 2666,
				"ecc": true,
				"registered": true,
				"condition": "used_working",
				"quantity": 4,
				"confidence": 0.92
			}`,
		}, nil).
		Once()

	extractor := extract.NewLLMExtractor(mockBackend)
	attrs, err := extractor.Extract(
		context.Background(),
		domain.ComponentRAM,
		"LOT OF 4 Samsung 32GB DDR4 ECC REG",
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, attrs)
	assert.Equal(t, float64(4), attrs["quantity"])
}

func TestLLMExtractor_Extract(t *testing.T) {
	t.Parallel()

	validRAMJSON := `{
		"manufacturer": "Samsung",
		"part_number": "M393A4K40CB2-CTD",
		"capacity_gb": 32,
		"quantity": 1,
		"generation": "DDR4",
		"speed_mhz": 2666,
		"ecc": true,
		"registered": true,
		"form_factor": "RDIMM",
		"rank": "2Rx4",
		"voltage": "1.2V",
		"condition": "used_working",
		"compatible_servers": [],
		"confidence": 0.95
	}`

	tests := []struct {
		name          string
		componentType domain.ComponentType
		title         string
		specs         map[string]string
		setupMock     func(*extractMocks.MockLLMBackend)
		wantErr       bool
		wantErrMsg    string
		wantAttrKey   string
		wantAttrVal   any
	}{
		{
			name:          "valid RAM extraction",
			componentType: domain.ComponentRAM,
			title:         "Samsung 32GB 2Rx4 PC4-2666V DDR4 ECC REG",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{Content: validRAMJSON}, nil).
					Once()
			},
			wantAttrKey: "manufacturer",
			wantAttrVal: "Samsung",
		},
		{
			name:          "invalid JSON from LLM",
			componentType: domain.ComponentRAM,
			title:         "Test",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.Anything).
					Return(extract.GenerateResponse{Content: "not json at all"}, nil).
					Once()
			},
			wantErr:    true,
			wantErrMsg: "parsing LLM JSON",
		},
		{
			name:          "out-of-range capacity_gb fails validation",
			componentType: domain.ComponentRAM,
			title:         "Test",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.Anything).
					Return(extract.GenerateResponse{
						Content: `{
							"capacity_gb": 2048,
							"generation": "DDR4",
							"condition": "used_working",
							"confidence": 0.9,
							"quantity": 1
						}`,
					}, nil).
					Once()
			},
			wantErr:    true,
			wantErrMsg: "capacity_gb",
		},
		{
			name:          "LLM returns capitalized condition",
			componentType: domain.ComponentRAM,
			title:         "Samsung 32GB DDR4-2666 ECC RDIMM",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{
						Content: `{
							"manufacturer": "Samsung",
							"capacity_gb": 32,
							"generation": "DDR4",
							"speed_mhz": 2666,
							"ecc": true,
							"registered": true,
							"condition": "New",
							"quantity": 1,
							"confidence": 0.95
						}`,
					}, nil).
					Once()
			},
			wantAttrKey: "condition",
			wantAttrVal: "new",
		},
		{
			name:          "LLM returns eBay-style condition string",
			componentType: domain.ComponentRAM,
			title:         "Hynix 16GB DDR4-2400 ECC REG",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{
						Content: `{
							"manufacturer": "Hynix",
							"capacity_gb": 16,
							"generation": "DDR4",
							"speed_mhz": 2400,
							"ecc": true,
							"registered": true,
							"condition": "Pre-Owned",
							"quantity": 1,
							"confidence": 0.88
						}`,
					}, nil).
					Once()
			},
			wantAttrKey: "condition",
			wantAttrVal: "used_working",
		},
		{
			name:          "LLM returns unrecognized condition defaults to unknown",
			componentType: domain.ComponentDrive,
			title:         "Seagate 4TB SAS 3.5 HDD",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{
						Content: `{
							"manufacturer": "Seagate",
							"capacity": "4TB",
							"interface": "SAS",
							"form_factor": "3.5",
							"type": "HDD",
							"condition": "Refurbished Grade A",
							"quantity": 1,
							"confidence": 0.85
						}`,
					}, nil).
					Once()
			},
			wantAttrKey: "condition",
			wantAttrVal: "unknown",
		},
		{
			name:          "backend error",
			componentType: domain.ComponentCPU,
			title:         "Test CPU",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.Anything).
					Return(extract.GenerateResponse{}, errors.New("connection refused")).
					Once()
			},
			wantErr:    true,
			wantErrMsg: "calling LLM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockBackend := extractMocks.NewMockLLMBackend(t)
			tt.setupMock(mockBackend)

			extractor := extract.NewLLMExtractor(mockBackend)
			attrs, err := extractor.Extract(
				context.Background(),
				tt.componentType,
				tt.title,
				tt.specs,
			)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, attrs)
			if tt.wantAttrKey != "" {
				assert.Equal(t, tt.wantAttrVal, attrs[tt.wantAttrKey])
			}
		})
	}
}

func TestLLMExtractor_ClassifyAndExtract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		title      string
		setupMock  func(*extractMocks.MockLLMBackend)
		wantType   domain.ComponentType
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:  "chains classify and extract",
			title: "Intel Xeon Gold 6130 SR3B0 2.1GHz",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				// First call: classify
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "" // classify does not use json format
					})).
					Return(extract.GenerateResponse{Content: "cpu"}, nil).
					Once()
				// Second call: extract
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{
						Content: `{
							"manufacturer": "Intel",
							"family": "Xeon",
							"model": "6130",
							"condition": "used_working",
							"confidence": 0.95,
							"quantity": 1
						}`,
					}, nil).
					Once()
			},
			wantType: domain.ComponentCPU,
		},
		{
			name:  "normalizes LLM condition through full pipeline",
			title: "Mellanox ConnectX-4 Lx 25GbE SFP28 Dual Port",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == ""
					})).
					Return(extract.GenerateResponse{Content: "nic"}, nil).
					Once()
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{
						Content: `{
							"manufacturer": "Mellanox",
							"model": "ConnectX-4 Lx",
							"speed": "25GbE",
							"port_count": 2,
							"port_type": "SFP28",
							"condition": "Used",
							"quantity": 1,
							"confidence": 0.93
						}`,
					}, nil).
					Once()
			},
			wantType: domain.ComponentNIC,
		},
		{
			name:  "classify error stops early",
			title: "Test",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.Anything).
					Return(extract.GenerateResponse{}, errors.New("timeout")).
					Once()
			},
			wantErr:    true,
			wantErrMsg: "classifying",
		},
		{
			name:  "extract error returns type but no attrs",
			title: "Samsung DDR4 32GB",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				// classify succeeds
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == ""
					})).
					Return(extract.GenerateResponse{Content: "ram"}, nil).
					Once()
				// extract fails
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{}, errors.New("LLM error")).
					Once()
			},
			wantErr:    true,
			wantErrMsg: "extracting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockBackend := extractMocks.NewMockLLMBackend(t)
			tt.setupMock(mockBackend)

			extractor := extract.NewLLMExtractor(mockBackend)
			ct, attrs, err := extractor.ClassifyAndExtract(
				context.Background(),
				tt.title,
				nil,
			)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantType, ct)
			require.NotNil(t, attrs)
		})
	}
}
