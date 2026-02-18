package credissuance

func Cred1() *LEARIssuanceRequestBody {
	return &LEARIssuanceRequestBody{
		Schema:        "LEARCredentialEmployee",
		OperationMode: "S",
		Format:        "jwt_vc_json",
		Payload: Payload{
			Mandator: Mandator{
				OrganizationIdentifier: "",
				Organization:           "",
				Country:                "",
				CommonName:             "",
				EmailAddress:           "",
			},
			Mandatee: Mandatee{
				FirstName:   "",
				LastName:    "",
				Nationality: "",
				Email:       "",
			},
			Power: []Power{
				{
					Type:     "domain",
					Domain:   "DOME",
					Function: "Onboarding",
					Action:   Strings{"execute"},
				},
			},
		},
	}
}

func Cred2() *LEARIssuanceRequestBody {
	return &LEARIssuanceRequestBody{
		Schema:        "LEARCredentialEmployee",
		OperationMode: "S",
		Format:        "jwt_vc_json",
		Payload: Payload{
			Mandator: Mandator{
				OrganizationIdentifier: "",
				Organization:           "",
				Country:                "",
				CommonName:             "",
				EmailAddress:           "",
			},
			Mandatee: Mandatee{
				FirstName:   "",
				LastName:    "",
				Nationality: "",
				Email:       "",
			},
			Power: []Power{
				{
					Type:     "domain",
					Domain:   "DOME",
					Function: "Onboarding",
					Action:   Strings{"execute"},
				},
			},
		},
	}
}
