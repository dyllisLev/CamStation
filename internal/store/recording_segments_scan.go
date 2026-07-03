package store

import "database/sql"

func scanRecordingSegment(row scanner) (RecordingSegment, error) {
	var segment RecordingSegment
	var tempPath, finalPath, backedUpAt, errorText sql.NullString
	var tsEnd sql.NullFloat64
	var fileSize sql.NullInt64
	if err := row.Scan(
		&segment.ID,
		&segment.CameraID,
		&segment.StreamName,
		&segment.Filename,
		&tempPath,
		&finalPath,
		&segment.TSStart,
		&tsEnd,
		&fileSize,
		&segment.Status,
		&segment.BackupState,
		&backedUpAt,
		&segment.BackupJobID,
		&errorText,
		&segment.CreatedAt,
		&segment.UpdatedAt,
	); err != nil {
		return RecordingSegment{}, err
	}
	if tempPath.Valid {
		segment.TempPath = tempPath.String
	}
	if finalPath.Valid {
		segment.FinalPath = finalPath.String
	}
	if tsEnd.Valid {
		value := tsEnd.Float64
		segment.TSEnd = &value
	}
	if fileSize.Valid {
		value := fileSize.Int64
		segment.FileSize = &value
	}
	if segment.BackupState == "" {
		segment.BackupState = "pending"
	}
	if backedUpAt.Valid {
		segment.BackedUpAt = backedUpAt.String
	}
	if errorText.Valid {
		segment.Error = errorText.String
	}
	return segment, nil
}
