// Thin HTTP adapter maintained alongside the notification API contract.
package notifications

type idsReq struct {
	IDs []string `json:"ids"`
}
