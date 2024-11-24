package model

import "github.com/asticode/go-astisub"

type StreamInfo struct {
	Index       int    `json:"index"`
	CodecType   string `json:"codec_type"`
	Language    string `json:"tags.language"`
	Description string `json:"tags.description"`
}
type FFprobeData struct {
	Streams []Stream `json:"streams"`
}
type Stream struct {
	Index              int    `json:"index"`
	CodecName          string `json:"codec_name"`
	CodecLongName      string `json:"codec_long_name"`
	Profile            string `json:"profile"`
	CodecType          string `json:"codec_type"`
	CodecTagString     string `json:"codec_tag_string"`
	CodecTag           string `json:"codec_tag"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	CodedWidth         int    `json:"coded_width"`
	CodedHeight        int    `json:"coded_height"`
	ClosedCaptions     int    `json:"closed_captions"`
	FilmGrain          int    `json:"film_grain"`
	HasBFrames         int    `json:"has_b_frames"`
	SampleAspectRatio  string `json:"sample_aspect_ratio"`
	DisplayAspectRatio string `json:"display_aspect_ratio"`
	PixelFormat        string `json:"pix_fmt"`
	Level              int    `json:"level"`
	ColorRange         string `json:"color_range"`
	ColorSpace         string `json:"color_space"`
	ColorTransfer      string `json:"color_transfer"`
	ColorPrimaries     string `json:"color_primaries"`
	ChromaLocation     string `json:"chroma_location"`
	FieldOrder         string `json:"field_order"`
	Refs               int    `json:"refs"`
	IsAVC              string `json:"is_avc"`
	NALLengthSize      string `json:"nal_length_size"`
	RFrameRate         string `json:"r_frame_rate"`
	AvgFrameRate       string `json:"avg_frame_rate"`
	TimeBase           string `json:"time_base"`
	StartPTS           int    `json:"start_pts"`
	StartTime          string `json:"start_time"`
	BitsPerRawSample   string `json:"bits_per_raw_sample"`
	ExtraDataSize      int    `json:"extradata_size"`
	Disposition        struct {
		Default         int `json:"default"`
		Dub             int `json:"dub"`
		Original        int `json:"original"`
		Comment         int `json:"comment"`
		Lyrics          int `json:"lyrics"`
		Karaoke         int `json:"karaoke"`
		Forced          int `json:"forced"`
		HearingImpaired int `json:"hearing_impaired"`
		VisualImpaired  int `json:"visual_impaired"`
		CleanEffects    int `json:"clean_effects"`
		AttachedPic     int `json:"attached_pic"`
		TimedThumbnails int `json:"timed_thumbnails"`
		Captions        int `json:"captions"`
		Descriptions    int `json:"descriptions"`
		Metadata        int `json:"metadata"`
		Dependent       int `json:"dependent"`
		StillImage      int `json:"still_image"`
	} `json:"disposition"`
	Tags struct {
		Language             string `json:"language"`
		Title                string `json:"title"`
		BPS                  string `json:"BPS"`
		Duration             string `json:"DURATION"`
		NumberOfFrames       string `json:"NUMBER_OF_FRAMES"`
		NumberOfBytes        string `json:"NUMBER_OF_BYTES"`
		StatisticsWritingApp string `json:"_STATISTICS_WRITING_APP"`
		StatisticsTags       string `json:"_STATISTICS_TAGS"`
	} `json:"tags"`
}

type ExtractedData struct {
	FileName   string
	EngSubPath string
	ChsSubPath string
	ChtSubPath string
	SdhSubPath string
}

type ExtractedStream struct {
	FileName     string
	FilePath     string
	EngSubBuffer []byte
	ChsSubBuffer []byte
	ChtSubBuffer []byte
	SdhSubBuffer []byte
}

type ParsedSubtitles struct {
	FileName      string
	FilePath      string
	EngSubtitle   *astisub.Subtitles
	ChsSubtitle   *astisub.Subtitles
	ChtSubtitle   *astisub.Subtitles
	SdhSubtitle   *astisub.Subtitles
	MergeSubtitle *astisub.Subtitles
	Translated    bool
	ExportedPath  string
}
