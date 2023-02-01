package sdk

/*
   Copyright 2016-2017 Alexander I.Grafov <grafov@gmail.com>

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.

   ॐ तारे तुत्तारे तुरे स्व
*/

// Org -
type Org struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// OrgUser -
// updated according to:
// https://grafana.com/docs/grafana/latest/http_api/org/#get-users-in-organization
type OrgUser struct {
	OrgID         uint   `json:"orgId"`
	ID            uint   `json:"userId"`
	Email         string `json:"email"`
	AvatarURL     string `json:"avatarUrl"`
	Login         string `json:"login"`
	Role          string `json:"role"`
	LastSeenAt    string `json:"lastSeenAt"`
	LastSeenAtAge string `json:"lastSeenAtAge"`
}
