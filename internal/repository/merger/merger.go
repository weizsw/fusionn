package merger

import (
	"bufio"
	"fmt"
	"fusionn/internal/consts"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
)

func Merge() error {
	lines1 := readFile("/Users/maverick/go/src/Github/fusionn/tmp/test.chi.srt")
	lines2 := readFile("/Users/maverick/go/src/Github/fusionn/tmp/test.eng.srt")

	var (
		i1, i2 int
		merged []string
	)
	index := 1

	s1TsLst, s1TsCodeMap, s1TsContentMap := parseSubtitles(lines1)
	s2TsLst, s2TsCodeMap, s2TsContentMap := parseSubtitles(lines2)
	s1TsCodeMap = unFragment(s1TsLst, s1TsCodeMap)
	s2TsCodeMap = unFragment(s2TsLst, s2TsCodeMap)

	for {
		if i1 >= len(s1TsLst) && i2 >= len(s2TsLst) {
			break
		}
		if i1 >= len(s1TsLst) {
			merged = append(merged, strconv.Itoa(index))
			index++
			merged = append(merged, s2TsCodeMap[s2TsLst[i2]])
			merged = append(merged, s2TsContentMap[s2TsLst[i2]])
			i2++
			continue
		}
		if i2 >= len(s2TsLst) {
			merged = append(merged, strconv.Itoa(index))
			index++
			merged = append(merged, s1TsCodeMap[s1TsLst[i1]])
			merged = append(merged, s1TsContentMap[s1TsLst[i1]])
			i1++
			continue
		}
		if s1TsLst[i1]-s2TsLst[i2] <= 1000 && s1TsLst[i1]-s2TsLst[i2] >= -1000 {
			merged = append(merged, strconv.Itoa(index))
			index++
			merged = append(merged, s1TsCodeMap[s1TsLst[i1]])
			merged = append(merged, s1TsContentMap[s1TsLst[i1]])
			merged = append(merged, s2TsContentMap[s2TsLst[i2]])
			i1++
			i2++
			continue
		}
		if s1TsLst[i1] < s2TsLst[i2] {
			merged = append(merged, strconv.Itoa(index))
			index++
			merged = append(merged, s1TsCodeMap[s1TsLst[i1]])
			merged = append(merged, s1TsContentMap[s1TsLst[i1]])
			i1++
			continue
		}
		merged = append(merged, strconv.Itoa(index))
		index++
		merged = append(merged, s2TsCodeMap[s2TsLst[i2]])
		merged = append(merged, s2TsContentMap[s2TsLst[i2]])
		i2++
		continue
	}
	// fmt.Println(merged)
	return writeFile(merged, "/Users/maverick/go/src/Github/fusionn/tmp/test.merged2.srt")
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

		tsCodeMap[tsLst[i]] = changeEndTimeLastThreeDigits(tsCodeMap[tsLst[i]], floorToThreeDigits(s1et))
		tsCodeMap[tsLst[j]] = changeStartTimeLastThreeDigits(tsCodeMap[tsLst[j]], ceilToThreeDigits(s2st))
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

func parseSubtitles(lines []string) ([]int, map[int]string, map[int]string) {
	var timestamps []int
	tsCodeMap := make(map[int]string)
	tsContentMap := make(map[int]string)

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
			tsContentMap[ts] += lines[i]
		}
		i++

	}

	return timestamps, tsCodeMap, tsContentMap
}

func readFile(filePath string) []string {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return nil
	}
	defer file.Close()

	// Create a scanner to read the file line by line
	scanner := bufio.NewScanner(file)

	// Read the file line by line and store in a []string slice
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
	}

	// Check for any scanning errors
	if err := scanner.Err(); err != nil {
		fmt.Println("Error scanning file:", err)
		return nil
	}

	return lines
}

func writeFile(lines []string, filePath string) error {
	// Open the file for writing
	file, err := os.Create(filePath)
	if err != nil {
		fmt.Println("Error creating file:", err)
		return err
	}
	defer file.Close()

	// Create a buffered writer
	writer := bufio.NewWriter(file)

	// Write each line to the file
	for _, line := range lines {
		_, err := writer.WriteString(line + "\n")
		if err != nil {
			fmt.Println("Error writing line:", err)
			return err
		}
	}

	// Flush the writer to ensure all data is written to the file
	err = writer.Flush()
	if err != nil {
		fmt.Println("Error flushing writer:", err)
		return err
	}

	fmt.Println("File written successfully.")
	return nil
}

func floorToThreeDigits(num int) int {
	return int(math.Floor(float64(num)/10.0)) * 10
}

func ceilToThreeDigits(num int) int {
	return int(math.Ceil(float64(num)/10.0)) * 10
}
