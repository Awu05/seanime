package transcoder

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/goccy/go-json"
)

// Custom hardware acceleration settings go straight into ffmpeg's arguments, so only known
// options with plain values and filters that only transform frames are accepted. Otherwise a
// settings change could make ffmpeg read or write arbitrary files.
//
// Ported from upstream Seanime's cassette hwaccel validation.

var customFlagValues = map[string]*regexp.Regexp{
	"-c:v":                   customTokenPattern,
	"-preset":                customTokenPattern,
	"-profile:v":             customTokenPattern,
	"-tune":                  customTunePattern,
	"-crf":                   customQualityPattern,
	"-pix_fmt":               customTokenPattern,
	"-rc:v":                  customTokenPattern,
	"-hwaccel":               customTokenPattern,
	"-hwaccel_output_format": customTokenPattern,
	"-hwaccel_device":        customDevicePattern,
	"-qsv_device":            customDevicePattern,
	"-sc_threshold":          customNumberPattern,
	"-async_depth":           customNumberPattern,
	"-look_ahead":            customNumberPattern,
	"-bf":                    customNumberPattern,
	"-low_power":             customNumberPattern,
	"-spatial-aq":            customNumberPattern,
	"-temporal-aq":           customNumberPattern,
	"-rc-lookahead":          customNumberPattern,
	"-delay":                 customNumberPattern,
	"-no-scenecut":           customNumberPattern,
}

var (
	customTokenPattern  = regexp.MustCompile(`^[a-z0-9_]+$`)
	customNumberPattern = regexp.MustCompile(`^-?[0-9]+$`)
	// a DRI render node, "auto", or a GPU index (e.g. "0" for CUDA)
	customDevicePattern  = regexp.MustCompile(`^(auto|[0-9]+|/dev/dri/[A-Za-z0-9_.-]+)$`)
	customTunePattern    = regexp.MustCompile(`^[a-z0-9_]+(,[a-z0-9_]+)*$`)
	customQualityPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	customFilePattern    = regexp.MustCompile(`(^|[=:])\s*/`)
	customSizePattern    = regexp.MustCompile(`%%|%(\[[12]\])?d`)
	customSizeArgPattern = regexp.MustCompile(`%(\[[12]\])?d`)

	// filter names that only transform video frames and take no file or url args
	customFilterNamePattern = regexp.MustCompile(`^(format|hwupload|hwdownload|scale|scale_[a-z]+)$`)
)

// ValidateCustomHwAccelSettings checks the JSON custom hardware acceleration settings saved in the
// mediastream settings, so a bad value is reported when it's saved rather than when transcoding
// silently falls back to CPU.
func ValidateCustomHwAccelSettings(customSettings string) error {
	if strings.TrimSpace(customSettings) == "" {
		return fmt.Errorf("custom hardware acceleration settings are empty")
	}
	var s HwAccelSettings
	if err := json.Unmarshal([]byte(customSettings), &s); err != nil {
		return fmt.Errorf("invalid custom hardware acceleration settings: %w", err)
	}
	return validateCustomHwAccel(&s)
}

// validateCustomHwAccel checks that custom settings only use known ffmpeg options and video filters.
func validateCustomHwAccel(s *HwAccelSettings) error {
	// The scale filter is formatted with the output width and height, so it must take exactly
	// those two %d placeholders and nothing else.
	if strings.Contains(customSizePattern.ReplaceAllString(s.ScaleFilter, ""), "%") {
		return fmt.Errorf("scale filter only supports %%d size placeholders")
	}
	if n := len(customSizeArgPattern.FindAllString(strings.ReplaceAll(s.ScaleFilter, "%%", ""), -1)); n != 2 {
		return fmt.Errorf("scale filter must take the width and height as two %%d placeholders, found %d", n)
	}
	if err := validateCustomFlags(s.DecodeFlags); err != nil {
		return err
	}
	if err := validateCustomFlags(s.EncodeFlags); err != nil {
		return err
	}
	return validateCustomFilter(s.ScaleFilter)
}

// validateCustomFlags checks that flags are option/value pairs with allowed options and plain values.
func validateCustomFlags(flags []string) error {
	if len(flags)%2 != 0 {
		return fmt.Errorf("ffmpeg flags must be option/value pairs")
	}
	for i := 0; i < len(flags); i += 2 {
		pattern, ok := customFlagValues[flags[i]]
		if !ok {
			return fmt.Errorf("unsupported ffmpeg option %q", flags[i])
		}
		if !pattern.MatchString(flags[i+1]) {
			return fmt.Errorf("invalid value %q for ffmpeg option %q", flags[i+1], flags[i])
		}
	}
	return nil
}

// validateCustomFilter checks that every filter in the chain is allowed. Filters are separated by "," or ";".
func validateCustomFilter(chain string) error {
	// check escaped option names too, while allowing division in expressions
	plain := strings.NewReplacer("\\", "", "'", "").Replace(chain)
	if customFilePattern.MatchString(plain) {
		return fmt.Errorf("video filters must not load options from files")
	}

	segments := strings.FieldsFunc(chain, func(r rune) bool { return r == ',' || r == ';' })
	for _, segment := range segments {
		name, _, _ := strings.Cut(segment, "=")
		if !customFilterNamePattern.MatchString(name) {
			return fmt.Errorf("unsupported video filter %q", name)
		}
	}
	return nil
}
