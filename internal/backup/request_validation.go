package backup

func ValidateStartRequestBoundary(request StartRequest) error {
	_, err := cleanPrefix(request.Prefix)
	return err
}
