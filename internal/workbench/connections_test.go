package workbench

import "testing"

func TestConnectionValidation(t *testing.T) {
	in, err := normalizeConnection(ConnectionInput{Name: "  Supplier feed  ", Method: "fictional"})
	if err != nil || in.Name != "Supplier feed" || in.MinimumCadenceSeconds != 30 || in.CadenceSeconds != 300 {
		t.Fatal(in, err)
	}
	for _, method := range []string{"fictional", "api_polling", "webhook", "browser", "public_scrape", "email"} {
		if _, err = normalizeConnection(ConnectionInput{Name: "Feed", Method: method, MinimumCadenceSeconds: 3600}); err != nil {
			t.Fatal(method, err)
		}
	}
	badID := "not-a-uuid"
	for _, in := range []ConnectionInput{{Name: "", Method: "fictional"}, {Name: "Bad\nname", Method: "fictional"}, {Name: "Feed", Method: "internal_api"}, {Name: "Feed", Method: "fictional", MinimumCadenceSeconds: 29}, {Name: "Feed", Method: "fictional", MinimumCadenceSeconds: 600, CadenceSeconds: 300}, {Name: "Feed", Method: "email", CadenceSeconds: 86401}, {Name: "Feed", Method: "fictional", SupplierID: &badID}, {Name: "Feed", Method: "fictional", BuyerUserID: &badID}} {
		if _, err = normalizeConnection(in); err != ErrInvalid {
			t.Fatal("invalid connection accepted", in, err)
		}
	}
}
