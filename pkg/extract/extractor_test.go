package extract_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	extractMocks "github.com/donaldgifford/server-price-tracker/pkg/extract/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// expectName configures the mock to satisfy the single Name() call that
// NewLLMExtractor makes at construction. The returned value becomes the
// backend label on emitted token metrics; tests that don't assert on
// metrics can ignore it.
func expectName(m *extractMocks.MockLLMBackend, name string) {
	m.EXPECT().Name().Return(name).Once()
}

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
					Return(extract.GenerateResponse{Content: "psu"}, nil).
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
			expectName(mockBackend, "test-backend")
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
	expectName(mockBackend, "test-backend")
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
	expectName(mockBackend, "test-backend")
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
						// 1500 is not divisible by 1024 or 1000, so the
						// MB/MiB normalizer cannot rescue it.
						Content: `{
							"capacity_gb": 1500,
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
		{
			name:          "JSON wrapped in ```json fences (Anthropic habit)",
			componentType: domain.ComponentRAM,
			title:         "Samsung 32GB DDR4-2666 ECC RDIMM",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{
						Content: "```json\n" + validRAMJSON + "\n```",
					}, nil).
					Once()
			},
			wantAttrKey: "manufacturer",
			wantAttrVal: "Samsung",
		},
		{
			name:          "JSON wrapped in bare ``` fences",
			componentType: domain.ComponentRAM,
			title:         "Samsung 32GB DDR4-2666 ECC RDIMM",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{
						Content: "```\n" + validRAMJSON + "\n```",
					}, nil).
					Once()
			},
			wantAttrKey: "manufacturer",
			wantAttrVal: "Samsung",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockBackend := extractMocks.NewMockLLMBackend(t)
			expectName(mockBackend, "test-backend")
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
		{
			name: "accessory short-circuit skips llm",
			title: "DELL EMC POWEREDGE R740xd 24 BAY SFF SERVER BACKPLANE " +
				"K2Y8N7 58D2W P1MJ3",
			setupMock: func(_ *extractMocks.MockLLMBackend) {
				// No Generate expectations — mockery fails the test if
				// the LLM is called.
			},
			wantType: domain.ComponentOther,
		},
		{
			name:  "gpu nvidia tesla p40 full pipeline",
			title: "NVIDIA Tesla P40 24GB GDDR5 GPU Accelerator",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == ""
					})).
					Return(extract.GenerateResponse{Content: "gpu"}, nil).
					Once()
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{
						Content: `{
							"manufacturer": "NVIDIA",
							"family": "Tesla",
							"model": "P40",
							"vram_gb": 24,
							"memory_type": "GDDR5",
							"condition": "used_working",
							"quantity": 1,
							"confidence": 0.94
						}`,
					}, nil).
					Once()
			},
			wantType: domain.ComponentGPU,
		},
		{
			name:  "gpu a100 vram unit confusion repaired",
			title: "NVIDIA A100 80GB SXM4 GPU",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == ""
					})).
					Return(extract.GenerateResponse{Content: "gpu"}, nil).
					Once()
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{
						Content: `{
							"manufacturer": "NVIDIA",
							"model": "A100",
							"vram_gb": 81920,
							"interface": "SXM4",
							"condition": "used_working",
							"quantity": 1,
							"confidence": 0.92
						}`,
					}, nil).
					Once()
			},
			wantType: domain.ComponentGPU,
		},
		{
			name:  "workstation dell precision t7920 full pipeline",
			title: "Dell Precision T7920 Workstation Xeon Gold 6248R 256GB",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == ""
					})).
					Return(extract.GenerateResponse{Content: "workstation"}, nil).
					Once()
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{
						Content: `{
								"vendor": "Dell",
								"line": "Precision",
								"model": "T7920",
								"cpu": "Xeon Gold 6248R",
								"ram_gb": 256,
								"form_factor": "tower",
								"condition": "used_working",
								"quantity": 1,
								"confidence": 0.92
							}`,
					}, nil).
					Once()
			},
			wantType: domain.ComponentWorkstation,
		},
		{
			name:  "desktop dell optiplex 7080 full pipeline",
			title: "Dell OptiPlex 7080 Micro i7-10700T 16GB 512GB SSD",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == ""
					})).
					Return(extract.GenerateResponse{Content: "desktop"}, nil).
					Once()
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{
						Content: `{
								"vendor": "Dell",
								"line": "OptiPlex",
								"model": "7080",
								"cpu": "i7-10700T",
								"ram_gb": 16,
								"storage_gb": 512,
								"form_factor": "micro",
								"condition": "used_working",
								"quantity": 1,
								"confidence": 0.9
							}`,
					}, nil).
					Once()
			},
			wantType: domain.ComponentDesktop,
		},
		{
			name:  "gpu amd instinct mi210 family canonicalised",
			title: "AMD Instinct MI210 64GB HBM2e GPU",
			setupMock: func(m *extractMocks.MockLLMBackend) {
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == ""
					})).
					Return(extract.GenerateResponse{Content: "gpu"}, nil).
					Once()
				m.EXPECT().
					Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
						return r.Format == "json"
					})).
					Return(extract.GenerateResponse{
						Content: `{
							"manufacturer": "AMD",
							"family": "Instinct",
							"model": "MI210",
							"vram_gb": 64,
							"memory_type": "HBM2e",
							"condition": "new",
							"quantity": 1,
							"confidence": 0.96
						}`,
					}, nil).
					Once()
			},
			wantType: domain.ComponentGPU,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockBackend := extractMocks.NewMockLLMBackend(t)
			expectName(mockBackend, "test-backend")
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
			if ct == domain.ComponentOther {
				assert.Equal(t, 0.95, attrs["confidence"],
					"short-circuit must mark confidence at 0.95")
			}
			// Spot-check GPU pipeline post-conditions: VRAM unit repair
			// (81920 → 80) and family canonicalisation (Tesla → tesla,
			// Instinct → instinct).
			if ct == domain.ComponentGPU {
				if vram, ok := attrs["vram_gb"].(int); ok {
					assert.LessOrEqual(t, vram, 256,
						"vram_gb must be normalised to ≤256 after repair")
				}
				if family, ok := attrs["family"].(string); ok && family != "" {
					assert.Equal(t, family, strings.ToLower(family),
						"family must be canonicalised to lowercase")
				}
			}
		})
	}
}

func TestLLMExtractor_ClassifyAndExtract_SystemPreClassHook(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		specs    map[string]string
		wantType domain.ComponentType
	}{
		{
			name:  "ThinkStation series short-circuits to workstation",
			title: "Lenovo P620 Threadripper 3995WX 256GB RTX 3090",
			specs: map[string]string{
				"Brand":  "Lenovo",
				"Series": "ThinkStation",
			},
			wantType: domain.ComponentWorkstation,
		},
		{
			name:  "OptiPlex series short-circuits to desktop",
			title: "Dell 7080 Tower i7-10700 16GB 512GB",
			specs: map[string]string{
				"Brand":  "Dell",
				"Series": "OptiPlex",
			},
			wantType: domain.ComponentDesktop,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockBackend := extractMocks.NewMockLLMBackend(t)
			expectName(mockBackend, "test-backend")
			// Only Extract is called — no Classify because pre-class hook
			// short-circuits. Mockery fails the test if Generate is called
			// with Format == "" (the classify call).
			mockBackend.EXPECT().
				Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
					return r.Format == "json"
				})).
				Return(extract.GenerateResponse{
					Content: `{
						"vendor": "Dell",
						"model": "T1234",
						"condition": "used_working",
						"quantity": 1,
						"confidence": 0.9
					}`,
				}, nil).
				Once()

			extractor := extract.NewLLMExtractor(mockBackend)
			ct, attrs, err := extractor.ClassifyAndExtract(
				context.Background(),
				tt.title,
				tt.specs,
			)
			require.NoError(t, err)
			assert.Equal(t, tt.wantType, ct)
			require.NotNil(t, attrs)
		})
	}
}

func TestLLMExtractor_TokenMetrics(t *testing.T) {
	t.Parallel()

	// Each subtest uses unique label values derived from its name so parallel
	// tests cannot collide on the global metric vec. No Reset() needed.
	const (
		promptTokens     = 250
		completionTokens = 5
		totalTokens      = promptTokens + completionTokens
	)

	classifyResp := extract.GenerateResponse{
		Content: "ram",
		Model:   "model-a",
		Usage: extract.TokenUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
		},
	}

	t.Run("Classify success increments token counters", func(t *testing.T) {
		t.Parallel()

		backend := "test-" + t.Name()
		model := "model-" + t.Name()

		mockBackend := extractMocks.NewMockLLMBackend(t)
		expectName(mockBackend, backend)
		resp := classifyResp
		resp.Model = model
		mockBackend.EXPECT().
			Generate(mock.Anything, mock.Anything).
			Return(resp, nil).
			Once()

		extractor := extract.NewLLMExtractor(mockBackend)
		_, err := extractor.Classify(context.Background(), "Samsung 32GB DDR4")
		require.NoError(t, err)

		gotInput := testutil.ToFloat64(
			metrics.ExtractionTokensTotal.WithLabelValues(backend, model, "input"),
		)
		gotOutput := testutil.ToFloat64(
			metrics.ExtractionTokensTotal.WithLabelValues(backend, model, "output"),
		)
		assert.InDelta(t, float64(promptTokens), gotInput, 0)
		assert.InDelta(t, float64(completionTokens), gotOutput, 0)

		histCount := testutil.CollectAndCount(metrics.ExtractionTokensPerRequest)
		assert.Positive(t, histCount, "histogram should have at least one observation")
	})

	t.Run("Extract success increments token counters", func(t *testing.T) {
		t.Parallel()

		backend := "test-" + t.Name()
		model := "model-" + t.Name()

		mockBackend := extractMocks.NewMockLLMBackend(t)
		expectName(mockBackend, backend)
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
					"quantity": 1,
					"confidence": 0.95
				}`,
				Model: model,
				Usage: extract.TokenUsage{
					PromptTokens:     promptTokens,
					CompletionTokens: completionTokens,
					TotalTokens:      totalTokens,
				},
			}, nil).
			Once()

		extractor := extract.NewLLMExtractor(mockBackend)
		_, err := extractor.Extract(
			context.Background(),
			domain.ComponentRAM,
			"Samsung 32GB DDR4",
			nil,
		)
		require.NoError(t, err)

		gotInput := testutil.ToFloat64(
			metrics.ExtractionTokensTotal.WithLabelValues(backend, model, "input"),
		)
		gotOutput := testutil.ToFloat64(
			metrics.ExtractionTokensTotal.WithLabelValues(backend, model, "output"),
		)
		assert.InDelta(t, float64(promptTokens), gotInput, 0)
		assert.InDelta(t, float64(completionTokens), gotOutput, 0)
	})

	t.Run("Backend error does not increment token counters", func(t *testing.T) {
		t.Parallel()

		backend := "test-" + t.Name()
		model := "model-" + t.Name()

		mockBackend := extractMocks.NewMockLLMBackend(t)
		expectName(mockBackend, backend)
		mockBackend.EXPECT().
			Generate(mock.Anything, mock.Anything).
			Return(extract.GenerateResponse{}, errors.New("connection refused")).
			Once()

		extractor := extract.NewLLMExtractor(mockBackend)
		_, err := extractor.Classify(context.Background(), "Samsung 32GB DDR4")
		require.Error(t, err)

		gotInput := testutil.ToFloat64(
			metrics.ExtractionTokensTotal.WithLabelValues(backend, model, "input"),
		)
		gotOutput := testutil.ToFloat64(
			metrics.ExtractionTokensTotal.WithLabelValues(backend, model, "output"),
		)
		assert.Zero(t, gotInput, "failed Generate must not increment input counter")
		assert.Zero(t, gotOutput, "failed Generate must not increment output counter")
	})

	t.Run("Bad JSON still records tokens (spend tracking)", func(t *testing.T) {
		t.Parallel()

		// The extract path emits metrics BEFORE json.Unmarshal so tokens-paid-for
		// are recorded even when the response was unparseable.
		backend := "test-" + t.Name()
		model := "model-" + t.Name()

		mockBackend := extractMocks.NewMockLLMBackend(t)
		expectName(mockBackend, backend)
		mockBackend.EXPECT().
			Generate(mock.Anything, mock.Anything).
			Return(extract.GenerateResponse{
				Content: "not json at all",
				Model:   model,
				Usage: extract.TokenUsage{
					PromptTokens:     promptTokens,
					CompletionTokens: completionTokens,
					TotalTokens:      totalTokens,
				},
			}, nil).
			Once()

		extractor := extract.NewLLMExtractor(mockBackend)
		_, err := extractor.Extract(
			context.Background(),
			domain.ComponentRAM,
			"Samsung 32GB DDR4",
			nil,
		)
		require.Error(t, err)

		gotInput := testutil.ToFloat64(
			metrics.ExtractionTokensTotal.WithLabelValues(backend, model, "input"),
		)
		assert.InDelta(t, float64(promptTokens), gotInput, 0,
			"tokens are billed even when response fails parse — emit before parse")
	})
}

func TestExtract_RAMNormalizesSpeed(t *testing.T) {
	t.Parallel()

	// LLM returns null for speed_mhz, but the title contains PC4-21300.
	// NormalizeRAMSpeed should fill in 2666.
	mockBackend := extractMocks.NewMockLLMBackend(t)
	expectName(mockBackend, "test-backend")
	mockBackend.EXPECT().
		Generate(mock.Anything, mock.MatchedBy(func(r extract.GenerateRequest) bool {
			return r.Format == "json"
		})).
		Return(extract.GenerateResponse{
			Content: `{
				"manufacturer": "Samsung",
				"capacity_gb": 32,
				"generation": "DDR4",
				"speed_mhz": null,
				"ecc": true,
				"registered": true,
				"condition": "used_working",
				"quantity": 1,
				"confidence": 0.9
			}`,
		}, nil).
		Once()

	extractor := extract.NewLLMExtractor(mockBackend)
	attrs, err := extractor.Extract(
		context.Background(),
		domain.ComponentRAM,
		"Samsung 32GB DDR4 PC4-21300 ECC REG",
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, attrs)
	assert.Equal(t, 2666, attrs["speed_mhz"], "speed_mhz should be normalized from PC4-21300")
}
