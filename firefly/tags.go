package firefly

type Tags struct {
	Tag         string  `json:"tag"`
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Zoom        string  `json:"zoom_level"`
}
