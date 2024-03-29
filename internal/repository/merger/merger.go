package merger

import (
	"fmt"
	"fusionn/internal/consts"
	"fusionn/internal/repository/common"
	"fusionn/pkg/deepl"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/longbridgeapp/opencc"
)

func Merge(filename, zhSubPath, engSubPath string) error {
	lines1, err := common.ReadFile(zhSubPath)
	if err != nil {
		return err
	}
	lines2, err := common.ReadFile(engSubPath)
	if err != nil {
		return err
	}

	var (
		i1, i2 int
		merged []string
	)
	index := 1

	s1TsLst, s1TsCodeMap, s1TsContentMap := parseSubtitles("zh", lines1)
	s2TsLst, s2TsCodeMap, s2TsContentMap := parseSubtitles("eng", lines2)

	for {
		if i1 >= len(s1TsLst) && i2 >= len(s2TsLst) {
			break
		}
		if i1 >= len(s1TsLst) {
			merged = append(merged, strconv.Itoa(index))
			index++
			merged = append(merged, s2TsCodeMap[s2TsLst[i2]])
			merged = append(merged, s2TsContentMap[s2TsLst[i2]])
			merged = append(merged, "")
			i2++
			continue
		}
		if i2 >= len(s2TsLst) {
			merged = append(merged, strconv.Itoa(index))
			index++
			merged = append(merged, s1TsCodeMap[s1TsLst[i1]])
			merged = append(merged, s1TsContentMap[s1TsLst[i1]])
			merged = append(merged, "")
			i1++

			continue
		}
		if s1TsLst[i1]-s2TsLst[i2] <= 1000 && s1TsLst[i1]-s2TsLst[i2] >= -1000 {
			merged = append(merged, strconv.Itoa(index))
			index++
			merged = append(merged, s1TsCodeMap[s1TsLst[i1]])
			merged = append(merged, s1TsContentMap[s1TsLst[i1]])
			merged = append(merged, s2TsContentMap[s2TsLst[i2]])
			merged = append(merged, "")
			i1++
			i2++
			continue
		}
		if s1TsLst[i1] < s2TsLst[i2] {
			merged = append(merged, strconv.Itoa(index))
			index++
			merged = append(merged, s1TsCodeMap[s1TsLst[i1]])
			merged = append(merged, s1TsContentMap[s1TsLst[i1]])
			merged = append(merged, "")
			i1++
			continue
		}
		merged = append(merged, strconv.Itoa(index))
		index++
		merged = append(merged, s2TsCodeMap[s2TsLst[i2]])
		merged = append(merged, s2TsContentMap[s2TsLst[i2]])
		merged = append(merged, "")
		i2++
		continue
	}
	subtitlePath, err := common.GetTmpSubtitleFullPath(filename + "." + consts.DUAL_LAN)
	if err != nil {
		return err
	}
	return common.WriteFile(merged, subtitlePath)
}

func TranslateAndMerge(filename, engSubPath string) error {
	log.Println("Using DeepL to translate English subtitles to Chinese...")
	// Read the English subtitles
	lines, err := common.ReadFile(engSubPath)
	if err != nil {
		return err
	}

	// Initialize the DeepL translator
	translator := deepl.NewDeepL()

	// Parse the English subtitles
	tsLst, tsCodeMap, tsContentMap := parseSubtitles("eng", lines)

	// Translate the English subtitles to Chinese
	tsTranslatedMap := make(map[int]string, len(tsContentMap))
	for i := 0; i < len(tsLst); i += 50 {
		var contents []string
		var timestamps []int
		for j := 0; j < 50 && i+j < len(tsLst); j++ {
			timestamps = append(timestamps, tsLst[i+j])
			contents = append(contents, tsContentMap[tsLst[i+j]])
		}

		translated, err := translator.Translate(contents, "zh", "en")
		if err != nil {
			log.Fatalf("Error translating subtitle: %s", err)
			return err
		}

		// Replace the English subtitles with the translated Chinese subtitles
		for k, translation := range translated.Translations {
			tsTranslatedMap[timestamps[k]] = translation.Text
		}
	}

	// Merge the original and translated subtitles
	var (
		i      int
		merged []string
	)
	index := 1
	for i < len(tsLst) {
		merged = append(merged, strconv.Itoa(index))
		index++
		merged = append(merged, tsCodeMap[tsLst[i]])
		merged = append(merged, tsTranslatedMap[tsLst[i]])
		merged = append(merged, tsContentMap[tsLst[i]])
		merged = append(merged, "")
		i++
	}

	subtitlePath, err := common.GetTmpSubtitleFullPath(filename + "." + consts.DUAL_LAN)
	if err != nil {
		return err
	}
	return common.WriteFile(merged, subtitlePath)
}

func unFragment(tsLst []int, tsCodeMap map[int]string) map[int]string {
	for i := 0; i < len(tsLst)-1; i++ {
		j := i + 1
		if j >= len(tsLst) {
			break
		}
		_, s1et := getLastThreeDigits(tsCodeMap[tsLst[i]])
		s2st, _ := getLastThreeDigits(tsCodeMap[tsLst[j]])
		if s1et < s2st {
			continue
		}

		tsCodeMap[tsLst[i]] = changeEndTimeLastThreeDigits(tsCodeMap[tsLst[i]], common.Floor(s1et))
		tsCodeMap[tsLst[j]] = changeStartTimeLastThreeDigits(tsCodeMap[tsLst[j]], common.Ceil(s2st))
	}
	return tsCodeMap
}

func getLastThreeDigits(timestamp string) (int, int) {
	// Regular expression pattern to match the timestamp line
	pattern := consts.TIME_CODE_PATTERN

	// Compile the regular expression pattern
	re := regexp.MustCompile(pattern)

	// Find the matches in the timestamp string
	matches := re.FindStringSubmatch(timestamp)

	// Extract the last three digits from the start and end times
	startLastThreeDigits, _ := strconv.Atoi(matches[4])
	endLastThreeDigits, _ := strconv.Atoi(matches[8])

	// Compare the last three digits
	return startLastThreeDigits, endLastThreeDigits
}

func changeStartTimeLastThreeDigits(timestamp string, newDigits int) string {
	// Regular expression pattern to match the timestamp line
	pattern := consts.TIME_CODE_PATTERN

	// Compile the regular expression pattern
	re := regexp.MustCompile(pattern)

	// Find the matches in the timestamp string
	matches := re.FindStringSubmatch(timestamp)

	// Extract the individual components from the matches
	startHour, _ := strconv.Atoi(matches[1])
	startMinute, _ := strconv.Atoi(matches[2])
	startSecond, _ := strconv.Atoi(matches[3])
	startLastThreeDigits, _ := strconv.Atoi(matches[4])

	endHour, _ := strconv.Atoi(matches[5])
	endMinute, _ := strconv.Atoi(matches[6])
	endSecond, _ := strconv.Atoi(matches[7])
	endLastThreeDigits, _ := strconv.Atoi(matches[8])

	// Update the last three digits with the new value
	startLastThreeDigits = newDigits

	// Format the updated timestamp
	updatedTimestamp := fmt.Sprintf("%02d:%02d:%02d,%03d --> %02d:%02d:%02d,%03d",
		startHour, startMinute, startSecond, startLastThreeDigits,
		endHour, endMinute, endSecond, endLastThreeDigits)

	// Replace the original timestamp with the updated one in the input string
	return re.ReplaceAllString(timestamp, updatedTimestamp)
}

func changeEndTimeLastThreeDigits(timestamp string, newDigits int) string {
	// Regular expression pattern to match the timestamp line
	pattern := consts.TIME_CODE_PATTERN

	// Compile the regular expression pattern
	re := regexp.MustCompile(pattern)

	// Find the matches in the timestamp string
	matches := re.FindStringSubmatch(timestamp)

	// Extract the individual components from the matches
	startHour, _ := strconv.Atoi(matches[1])
	startMinute, _ := strconv.Atoi(matches[2])
	startSecond, _ := strconv.Atoi(matches[3])
	startLastThreeDigits, _ := strconv.Atoi(matches[4])

	endHour, _ := strconv.Atoi(matches[5])
	endMinute, _ := strconv.Atoi(matches[6])
	endSecond, _ := strconv.Atoi(matches[7])
	endLastThreeDigits, _ := strconv.Atoi(matches[8])

	// Update the last three digits with the new value
	endLastThreeDigits = newDigits

	// Format the updated timestamp
	updatedTimestamp := fmt.Sprintf("%02d:%02d:%02d,%03d --> %02d:%02d:%02d,%03d",
		startHour, startMinute, startSecond, startLastThreeDigits,
		endHour, endMinute, endSecond, endLastThreeDigits)

	// Replace the original timestamp with the updated one in the input string
	return re.ReplaceAllString(timestamp, updatedTimestamp)
}

func parseTimestamp(line string) (int, bool) {
	if !strings.Contains(line, "-->") {
		// Not a timestamp line
		return 0, false
	}
	parts := strings.Split(line, " --> ")
	start := parts[0]
	// end := parts[1]

	timeParts := strings.Split(start, ":")
	secondsAndMillis := strings.Split(timeParts[2], ",")

	// Extract hours, minutes, seconds, and milliseconds
	hours, _ := strconv.Atoi(timeParts[0])
	minutes, _ := strconv.Atoi(timeParts[1])
	seconds, _ := strconv.Atoi(secondsAndMillis[0])
	milliseconds, _ := strconv.Atoi(secondsAndMillis[1])

	// Calculate the total duration in milliseconds
	totalMillis := (hours * 3600 * 1000) + (minutes * 60 * 1000) + (seconds * 1000) + milliseconds

	return totalMillis, true
}

func parseSubtitles(lan string, lines []string) ([]int, map[int]string, map[int]string) {
	timestamps := []int{}
	tsCodeMap := make(map[int]string)
	tsContentMap := make(map[int]string)
	t2s, err := opencc.New("t2s")
	if err != nil {
		log.Fatal(err)
	}
	for i := 0; i < len(lines); i++ {
		ts, ok := parseTimestamp(lines[i])
		if !ok {
			continue
		}
		timestamps = append(timestamps, ts)
		tsCodeMap[ts] = lines[i]
		for {
			i++
			if i >= len(lines) {
				break
			}
			if len(strings.TrimSpace(lines[i])) == 0 {
				break
			}
			if lan == "zh" {
				out, err := t2s.Convert(lines[i])
				if err != nil {
					log.Fatal(err)
				}
				if _, ok := tsContentMap[ts]; !ok {
					tsContentMap[ts] += out
					continue
				}
				tsContentMap[ts] += " " + out
				continue
			}
			if _, ok := tsContentMap[ts]; !ok {
				tsContentMap[ts] += lines[i]
				continue
			}
			tsContentMap[ts] += " " + lines[i]
		}
		i++

	}

	return timestamps, tsCodeMap, tsContentMap
}

func replaceStartTimestamp(timeStr string) string {
	pattern := consts.TIME_CODE_PATTERN_II
	replacement := `${1}999 --> ${3}`

	re := regexp.MustCompile(pattern)
	modifiedTimeStr := re.ReplaceAllString(timeStr, replacement)
	return modifiedTimeStr
}

func replaceEndTimestamp(timeStr string) string {
	pattern := consts.TIME_CODE_PATTERN_III
	replacement := `${1} --> ${2}998`

	re := regexp.MustCompile(pattern)
	modifiedTimeStr := re.ReplaceAllString(timeStr, replacement)
	return modifiedTimeStr
}
