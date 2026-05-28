package application

import (
	"testing"
)

func TestZoomFactor(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{
			name:     "Zero zoom",
			input:    0,
			expected: 1.0,
		},
		{
			name:     "Negative zoom",
			input:    -10,
			expected: 1.0,
		},
		{
			name:     "100 percent zoom",
			input:    100,
			expected: 2.0,
		},
		{
			name:     "50 percent zoom",
			input:    50,
			expected: 1.5,
		},
		{
			name:     "500 percent zoom",
			input:    500,
			expected: 6.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := zoomFactor(tt.input)
			if actual != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, actual)
			}
		})
	}
}

func TestValidateVertical(t *testing.T) {
	tests := []struct {
		name      string
		input     VerticalRequest
		expectErr bool
	}{
		{
			name: "Valid request minimal",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
			},
			expectErr: false,
		},
		{
			name: "Empty InputURL",
			input: VerticalRequest{
				InputURL: "",
			},
			expectErr: true,
		},
		{
			name: "ScrollX under limit",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				ScrollX:  -101,
			},
			expectErr: true,
		},
		{
			name: "ScrollX over limit",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				ScrollX:  101,
			},
			expectErr: true,
		},
		{
			name: "Zoom under limit",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				Zoom:     -1,
			},
			expectErr: true,
		},
		{
			name: "Zoom over limit",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				Zoom:     501,
			},
			expectErr: true,
		},
		{
			name: "Invalid Keyframe Time",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				Keyframes: []Keyframe{
					{Time: -1},
				},
			},
			expectErr: true,
		},
		{
			name: "Invalid Keyframe ScrollX under limit",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				Keyframes: []Keyframe{
					{Time: 1, ScrollX: float64Ptr(-101)},
				},
			},
			expectErr: true,
		},
		{
			name: "Invalid Keyframe ScrollX over limit",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				Keyframes: []Keyframe{
					{Time: 1, ScrollX: float64Ptr(101)},
				},
			},
			expectErr: true,
		},
		{
			name: "Invalid Keyframe Zoom under limit",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				Keyframes: []Keyframe{
					{Time: 1, Zoom: float64Ptr(-1)},
				},
			},
			expectErr: true,
		},
		{
			name: "Invalid Keyframe Zoom over limit",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				Keyframes: []Keyframe{
					{Time: 1, Zoom: float64Ptr(501)},
				},
			},
			expectErr: true,
		},
		{
			name: "Both Duration and End defined",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				Duration: 10,
				End:      20,
			},
			expectErr: true,
		},
		{
			name: "End less or equal to Start",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				Start:    10,
				End:      5,
			},
			expectErr: true,
		},
		{
			name: "Valid CRF zero",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				CRF:      0,
			},
			expectErr: false,
		},
		{
			name: "Invalid CRF over limit",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				CRF:      52,
			},
			expectErr: true,
		},
		{
			name: "Invalid CRF negative",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				CRF:      -1,
			},
			expectErr: true,
		},
		{
			name: "Invalid Preset",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				Preset:   "invalid_preset",
			},
			expectErr: true,
		},
		{
			name: "Valid Preset",
			input: VerticalRequest{
				InputURL: "http://example.com/video.mp4",
				Preset:   "fast",
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateVertical(tt.input)
			if (err != nil) != tt.expectErr {
				t.Errorf("expected error: %v, got: %v", tt.expectErr, err)
			}
		})
	}
}

func float64Ptr(f float64) *float64 {
	return &f
}
