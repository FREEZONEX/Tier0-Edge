package sourcestream

import (
	"regexp"
	"strings"
	"time"
)

type EdgePlexSAPFilters struct {
	JobID        string    `json:"jobId,omitempty"`
	BeginDate    string    `json:"beginDate,omitempty"`
	EndDate      string    `json:"endDate,omitempty"`
	Material     string    `json:"material,omitempty"`
	BaseUnit     string    `json:"baseUnit,omitempty"`
	SelectFields *[]string `json:"selectFields,omitempty"`
	Order        string    `json:"order,omitempty"`
}

var plexJobID = regexp.MustCompile("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")
var sapMaterial = regexp.MustCompile("^[0-9]{18}$")
var sapUnit = regexp.MustCompile("^[A-Z0-9]{1,8}$")

func (f EdgePlexSAPFilters) Valid(ref string) bool {
	if f.JobID != "" && ((ref != "vendor:plex-mes:production" && ref != "vendor:plex-mes:operations") || !plexJobID.MatchString(f.JobID)) {
		return false
	}
	if ref == "vendor:plex-mes:operations" && f.JobID == "" {
		return false
	}
	if ref == "vendor:plex-mes:production" || f.BeginDate != "" || f.EndDate != "" {
		a, e := time.Parse(time.RFC3339Nano, f.BeginDate)
		b, e2 := time.Parse(time.RFC3339Nano, f.EndDate)
		if ref != "vendor:plex-mes:production" || e != nil || e2 != nil || !strings.HasSuffix(f.BeginDate, "Z") || !strings.HasSuffix(f.EndDate, "Z") || b.Before(a) || b.Sub(a) > 24*time.Hour {
			return false
		}
	}
	if f.SelectFields == nil && f.Material == "" && f.BaseUnit == "" && f.Order == "" {
		return true
	}
	if ref != "vendor:sap-odata:entity:Materials" || (f.Material != "" && !sapMaterial.MatchString(f.Material)) || (f.BaseUnit != "" && !sapUnit.MatchString(f.BaseUnit)) || (f.Order != "" && f.Order != "materialAsc" && f.Order != "materialDesc") {
		return false
	}
	if f.SelectFields != nil {
		seen := map[string]bool{}
		for _, v := range *f.SelectFields {
			if (v != "Material" && v != "Description" && v != "BaseUnit") || seen[v] {
				return false
			}
			seen[v] = true
		}
		return seen["Material"]
	}
	return true
}
