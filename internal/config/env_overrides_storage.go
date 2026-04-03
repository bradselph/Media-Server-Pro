package config

func (m *Manager) applyStorageEnvOverrides() {
	if val := envGetStr("STORAGE_BACKEND"); val != "" {
		m.config.Storage.Backend = val
	}
	if val := envGetStr("S3_ENDPOINT"); val != "" {
		m.config.Storage.S3.Endpoint = val
	}
	if val := envGetStr("S3_REGION"); val != "" {
		m.config.Storage.S3.Region = val
	}
	if val := envGetStr("S3_ACCESS_KEY_ID"); val != "" {
		m.config.Storage.S3.AccessKeyID = val
	}
	if val := envGetStr("S3_SECRET_ACCESS_KEY"); val != "" {
		m.config.Storage.S3.SecretAccessKey = val
	}
	if val := envGetStr("S3_BUCKET"); val != "" {
		m.config.Storage.S3.Bucket = val
	}
	if val, ok := envGetBool("S3_USE_PATH_STYLE"); ok {
		m.config.Storage.S3.UsePathStyle = val
	}
}
