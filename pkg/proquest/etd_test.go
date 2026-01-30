package proquest

import (
	"testing"
)

func TestEmbargoDate(t *testing.T) {
	tests := []struct {
		name           string
		submission     DISSSubmission
		expectedResult string
		description    string
	}{
		{
			name: "Indefinite embargo",
			submission: DISSSubmission{
				EmbargoCode: 0,
				Description: DISSDescription{
					Dates: DISSDates{
						AcceptDate: "05/01/2021",
					},
				},
				Repository: DISSRepository{
					Embargo: "NEVER DELIVER",
				},
			},
			expectedResult: "2999-12-31",
			description:    "When embargo is set to never deliver, should return Lehigh's special indefinite embargo date",
		},
		{
			name: "No embargo - code 0",
			submission: DISSSubmission{
				EmbargoCode: 0,
				Description: DISSDescription{
					Dates: DISSDates{
						AcceptDate: "05/01/2021",
					},
				},
				Repository: DISSRepository{
					Embargo: "",
				},
			},
			expectedResult: "",
			description:    "When embargo code is 0, should return empty string",
		},
		{
			name: "6 month embargo - code 1",
			submission: DISSSubmission{
				EmbargoCode: 1,
				Description: DISSDescription{
					Dates: DISSDates{
						AcceptDate: "05/01/2021",
					},
				},
				Repository: DISSRepository{
					Embargo: "",
				},
			},
			expectedResult: "2021-10-28",
			description:    "6 month embargo should add ~180 days",
		},
		{
			name: "12 month embargo - code 2",
			submission: DISSSubmission{
				EmbargoCode: 2,
				Description: DISSDescription{
					Dates: DISSDates{
						AcceptDate: "05/01/2021",
					},
				},
				Repository: DISSRepository{
					Embargo: "",
				},
			},
			expectedResult: "2022-04-26",
			description:    "12 month embargo (code 2) should add ~360 days",
		},
		{
			name: "12 month embargo - code 3",
			submission: DISSSubmission{
				EmbargoCode: 3,
				Description: DISSDescription{
					Dates: DISSDates{
						AcceptDate: "05/01/2021",
					},
				},
				Repository: DISSRepository{
					Embargo: "",
				},
			},
			expectedResult: "2022-04-26",
			description:    "12 month embargo (code 3) should add ~360 days",
		},
		{
			name: "Repository embargo date takes precedence",
			submission: DISSSubmission{
				EmbargoCode: 2,
				Description: DISSDescription{
					Dates: DISSDates{
						AcceptDate: "05/01/2021",
					},
				},
				Repository: DISSRepository{
					Embargo: "2023-12-25 some additional text",
				},
			},
			expectedResult: "2023-12-25",
			description:    "Repository embargo date should override embargo code calculation",
		},
		{
			name: "Repository embargo date only (no extra text)",
			submission: DISSSubmission{
				EmbargoCode: 1,
				Description: DISSDescription{
					Dates: DISSDates{
						AcceptDate: "05/01/2021",
					},
				},
				Repository: DISSRepository{
					Embargo: "2024-01-15",
				},
			},
			expectedResult: "2024-01-15",
			description:    "Repository embargo date without additional text",
		},
		{
			name: "Invalid completion date format",
			submission: DISSSubmission{
				EmbargoCode: 1,
				Description: DISSDescription{
					Dates: DISSDates{
						AcceptDate: "invalid-date",
					},
				},
				Repository: DISSRepository{
					Embargo: "",
				},
			},
			expectedResult: "",
			description:    "Invalid completion date should return empty string",
		},
		{
			name: "Invalid repository embargo date format",
			submission: DISSSubmission{
				EmbargoCode: 1,
				Description: DISSDescription{
					Dates: DISSDates{
						AcceptDate: "05/01/2021",
					},
				},
				Repository: DISSRepository{
					Embargo: "not-a-date some text",
				},
			},
			expectedResult: "2021-10-28",
			description:    "Invalid repository embargo date should fall back to embargo code",
		},
		{
			name: "Unknown embargo code",
			submission: DISSSubmission{
				EmbargoCode: 99, // Unknown code
				Description: DISSDescription{
					Dates: DISSDates{
						AcceptDate: "05/01/2021",
					},
				},
				Repository: DISSRepository{
					Embargo: "",
				},
			},
			expectedResult: "2021-05-01",
			description:    "Unknown embargo code should add no time",
		},
		{
			name: "Different completion year",
			submission: DISSSubmission{
				EmbargoCode: 2,
				Description: DISSDescription{
					Dates: DISSDates{
						AcceptDate: "12/01/2020",
					},
				},
				Repository: DISSRepository{
					Embargo: "",
				},
			},
			expectedResult: "2021-11-26",
			description:    "Should work with different completion years",
		},
		{
			name: "Empty completion date",
			submission: DISSSubmission{
				EmbargoCode: 1,
				Description: DISSDescription{
					Dates: DISSDates{
						AcceptDate: "",
					},
				},
				Repository: DISSRepository{
					Embargo: "",
				},
			},
			expectedResult: "",
			description:    "Empty completion date should return empty string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.submission.EmbargoDate()
			if result != tt.expectedResult {
				t.Errorf("EmbargoDate() = %v, want %v\nDescription: %s",
					result, tt.expectedResult, tt.description)
			}
		})
	}
}
