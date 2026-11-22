package locale

// Catalogs are declared in code so keys stay typed and parity is a
// simple map comparison. Keep ru and en key sets identical.
var catalogs = map[Lang]map[string]string{
	EN: {
		"err.invalid":       "invalid request",
		"err.invalid-arg":   "invalid argument {field}",
		"err.not-found":     "not found",
		"err.unauthorized":  "unauthorized",
		"err.forbidden":     "forbidden",
		"err.conflict":      "conflict",
		"err.stale":         "stale state, retry",
		"err.unavailable":   "service unavailable",
		"err.uncertain":     "outcome uncertain, retry with the same request",
		"err.decode":        "cannot decode request body",
		"err.body-too-big":  "request body too large",
		"err.machine-state": "invalid state transition",
		"err.migration":     "migration failed",
		"err.lease":         "lease conflict",
		"err.seal":          "cannot open sealed value",
		"err.token":         "bad token",
	},
	RU: {
		"err.invalid":       "некорректный запрос",
		"err.invalid-arg":   "некорректный аргумент {field}",
		"err.not-found":     "не найдено",
		"err.unauthorized":  "не авторизован",
		"err.forbidden":     "запрещено",
		"err.conflict":      "конфликт",
		"err.stale":         "устаревшее состояние, повторите",
		"err.unavailable":   "сервис недоступен",
		"err.uncertain":     "исход неизвестен, повторите тот же запрос",
		"err.decode":        "не удалось разобрать тело запроса",
		"err.body-too-big":  "слишком большое тело запроса",
		"err.machine-state": "недопустимый переход состояния",
		"err.migration":     "миграция не удалась",
		"err.lease":         "конфликт лизы",
		"err.seal":          "не удалось расшифровать значение",
		"err.token":         "некорректный токен",
	},
}
