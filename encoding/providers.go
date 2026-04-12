package encoding

// ProvideContentType provides a ContentType from a Config.
func ProvideContentType(cfg Config) ContentType {
	return contentTypeFromString(cfg.ContentType)
}
