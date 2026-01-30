package islandora

type Contributor struct {
	Name        string `json:"name"`
	Orcid       string `json:"orcid,omitempty"`
	Institution string `json:"institution,omitempty"`
	Email       string `json:"email,omitempty"`
	Status      string `json:"status,omitempty"`
}
