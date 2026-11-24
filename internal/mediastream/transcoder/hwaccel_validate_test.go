package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateCustomHwAccelAcceptsBuiltInProfiles(t *testing.T) {
	for _, kind := range []string{"disabled", "vaapi", "qsv", "qsv-low-power", "nvidia", "videotoolbox"} {
		s := GetHardwareAccelSettings(HwAccelOptions{Kind: kind, Preset: "fast"})
		assert.NoError(t, validateCustomHwAccel(&s), kind)
	}
}

func TestValidateCustomHwAccelRejects(t *testing.T) {
	tests := []struct {
		name     string
		settings HwAccelSettings
	}{
		{
			name: "output file and muxer options",
			settings: HwAccelSettings{
				EncodeFlags: []string{"-c:v", "libx264", "-f", "mpegts", "-y", "/tmp/WRITE_PROOF.ts"},
				ScaleFilter: "scale=%d:%d",
			},
		},
		{
			name: "unknown option",
			settings: HwAccelSettings{
				EncodeFlags: []string{"-c:v", "libx264", "-x264-params", "a=b"},
				ScaleFilter: "scale=%d:%d",
			},
		},
		{
			name: "dangling option without value",
			settings: HwAccelSettings{
				EncodeFlags: []string{"-c:v", "libx264", "-preset"},
				ScaleFilter: "scale=%d:%d",
			},
		},
		{
			name: "option as value",
			settings: HwAccelSettings{
				EncodeFlags: []string{"-c:v", "-i"},
				ScaleFilter: "scale=%d:%d",
			},
		},
		{
			name: "device outside dri",
			settings: HwAccelSettings{
				DecodeFlags: []string{"-hwaccel_device", "/etc/passwd"},
				ScaleFilter: "scale=%d:%d",
			},
		},
		{
			name:     "file reading filter",
			settings: HwAccelSettings{ScaleFilter: "scale=%d:%d,movie=/etc/passwd"},
		},
		{
			name:     "filter chain separated by semicolon",
			settings: HwAccelSettings{ScaleFilter: "scale=%d:%d;movie=/etc/passwd"},
		},
		{
			// formatting a height of 47 with %c would produce a slash
			name:     "non-size format verb",
			settings: HwAccelSettings{ScaleFilter: "scale=%[2]cw=width.txt:h=16"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Error(t, validateCustomHwAccel(&tt.settings))
		})
	}
}

func TestValidateCustomFilterRejectsFileOptions(t *testing.T) {
	for _, filter := range []string{
		"scale=/w=/tmp/width:h=16",
		"scale=w=16:/h=/tmp/height",
		"scale='/w'=/tmp/width:h=16",
		`scale=\/w=/tmp/width:h=16`,
		"scale=w=16: '/h'=/tmp/height",
		`scale=w=16:\'/h\'=/tmp/height`,
		"format=/pix_fmts=/tmp/format",
	} {
		assert.Error(t, validateCustomHwAccel(&HwAccelSettings{ScaleFilter: filter}), filter)
	}
	for _, filter := range []string{"scale=%d:%d", "scale=%[1]d:%[2]d"} {
		assert.NoError(t, validateCustomHwAccel(&HwAccelSettings{ScaleFilter: filter}), filter)
	}
	// division is fine inside a filter, it just isn't a valid scale filter on its own
	assert.NoError(t, validateCustomFilter("scale=iw/2:ih/2"))
}

// The scale filter is formatted with the width and height, so anything but two size placeholders
// would hand ffmpeg a broken -vf value.
func TestValidateCustomHwAccelRequiresTwoSizePlaceholders(t *testing.T) {
	for _, filter := range []string{"", "scale=iw/2:ih/2", "scale=%d:-2", "scale=%d:%d:%d", "scale=%%d:%%d"} {
		assert.Error(t, validateCustomHwAccel(&HwAccelSettings{ScaleFilter: filter}), filter)
	}
}

func TestValidateCustomHwAccelAllowsGPUIndexDevice(t *testing.T) {
	s := HwAccelSettings{
		DecodeFlags: []string{"-hwaccel", "cuda", "-hwaccel_device", "0"},
		EncodeFlags: []string{"-c:v", "h264_nvenc"},
		ScaleFilter: "scale_cuda=%d:%d",
	}
	assert.NoError(t, validateCustomHwAccel(&s))
}

func TestValidateCustomHwAccelSettings(t *testing.T) {
	assert.NoError(t, ValidateCustomHwAccelSettings(`{"encodeFlags":["-c:v","libx264"],"scaleFilter":"scale=%d:%d"}`))
	assert.Error(t, ValidateCustomHwAccelSettings(""))
	assert.Error(t, ValidateCustomHwAccelSettings("{not json"))
	assert.Error(t, ValidateCustomHwAccelSettings(`{"encodeFlags":["-y","/tmp/x"],"scaleFilter":"scale=%d:%d"}`))
}

func TestGetHardwareAccelSettingsCustom(t *testing.T) {
	t.Run("uses valid custom settings", func(t *testing.T) {
		s := GetHardwareAccelSettings(HwAccelOptions{
			Kind:           "custom",
			CustomSettings: `{"decodeFlags":[],"encodeFlags":["-c:v","libx264","-crf","20.5","-tune","fastdecode,zerolatency"],"scaleFilter":"scale=%d:%d"}`,
		})
		assert.Equal(t, "custom", s.Name)
		assert.Contains(t, s.EncodeFlags, "20.5")
	})

	t.Run("falls back to CPU for a file-writing payload", func(t *testing.T) {
		s := GetHardwareAccelSettings(HwAccelOptions{
			Kind:           "custom",
			Preset:         "fast",
			CustomSettings: `{"decodeFlags":[],"encodeFlags":["-c:v","libx264","-preset","ultrafast","-f","mpegts","-y","/tmp/WRITE_PROOF.ts"],"scaleFilter":"scale=%d:%d"}`,
		})
		assert.Equal(t, "disabled", s.Name)
		assert.NotContains(t, s.EncodeFlags, "/tmp/WRITE_PROOF.ts")
	})
}
