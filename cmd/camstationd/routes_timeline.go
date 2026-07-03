package main

import (
	"fmt"
	"strconv"
	"time"

	"camstation/internal/store"
)

func layoutID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func dayRangeKST(date string) (time.Time, time.Time, error) {
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		location = time.FixedZone("KST", 9*60*60)
	}
	start, err := time.ParseInLocation("2006-01-02", date, location)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid date format; expected YYYY-MM-DD")
	}
	return start, start.Add(24 * time.Hour), nil
}

func timelineSegments(segments []store.RecordingSegment) []map[string]any {
	out := make([]map[string]any, 0, len(segments))
	for _, segment := range segments {
		out = append(out, map[string]any{
			"camera_id": segment.StreamName,
			"filename":  segment.Filename,
			"ts_start":  segment.TSStart,
			"ts_end":    segment.TSEnd,
			"file_size": segment.FileSize,
			"status":    segment.Status,
		})
	}
	return out
}
