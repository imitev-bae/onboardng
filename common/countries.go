package common

type Country struct {
	Code string
	Name string
}

var Countries = []Country{
	{Code: "US", Name: "United States"},
	{Code: "GB", Name: "United Kingdom"},
	{Code: "CA", Name: "Canada"},
	{Code: "AU", Name: "Australia"},
	{Code: "DE", Name: "Germany"},
	{Code: "FR", Name: "France"},
	{Code: "ES", Name: "Spain"},
	{Code: "IT", Name: "Italy"},
	{Code: "NL", Name: "Netherlands"},
	{Code: "BE", Name: "Belgium"},
	{Code: "CH", Name: "Switzerland"},
	{Code: "AT", Name: "Austria"},
	{Code: "SE", Name: "Sweden"},
	{Code: "NO", Name: "Norway"},
	{Code: "DK", Name: "Denmark"},
	{Code: "FI", Name: "Finland"},
	{Code: "IE", Name: "Ireland"},
	{Code: "PT", Name: "Portugal"},
	{Code: "GR", Name: "Greece"},
	{Code: "LU", Name: "Luxembourg"},
	{Code: "JP", Name: "Japan"},
	{Code: "CN", Name: "China"},
	{Code: "IN", Name: "India"},
	{Code: "BR", Name: "Brazil"},
	{Code: "MX", Name: "Mexico"},
	{Code: "ZA", Name: "South Africa"},
	{Code: "AE", Name: "United Arab Emirates"},
	{Code: "SG", Name: "Singapore"},
	{Code: "KR", Name: "South Korea"},
	{Code: "NZ", Name: "New Zealand"},
}

func GetCountryName(code string) string {
	for _, c := range Countries {
		if c.Code == code {
			return c.Name
		}
	}
	return ""
}

func IsValidCountry(code string) bool {
	for _, c := range Countries {
		if c.Code == code {
			return true
		}
	}
	return false
}
