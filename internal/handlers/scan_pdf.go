package handlers

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// dangerousKeys are PDF dictionary keys that indicate executable, scripting,
// or auto-action content. OpenStore rejects rather than sanitises — presence
// of any of these is sufficient reason to distrust the upload.
var dangerousKeys = []string{
	"/JS",
	"/JavaScript",
	"/EmbeddedFile",
	"/OpenAction",
	"/Launch",
}

// scanPDFForDangerousElements parses the PDF object model and walks every
// indirect object looking for dangerous dictionary keys. Returns an error
// naming the first dangerous element found.
func scanPDFForDangerousElements(data []byte) error {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed

	ctx, err := api.ReadContext(bytes.NewReader(data), conf)
	if err != nil {
		return fmt.Errorf("pdf parse error: %w", err)
	}

	if err := api.ValidateContext(ctx); err != nil {
		// Validation failure on a relaxed parse means the PDF is structurally
		// suspect. Fail closed.
		return fmt.Errorf("pdf validation failed: %w", err)
	}

	for objNr, entry := range ctx.XRefTable.Table {
		if entry == nil || entry.Object == nil {
			continue
		}

		dict, ok := entry.Object.(types.Dict)
		if !ok {
			continue
		}

		for _, key := range dangerousKeys {
			// pdfcpu Dict keys do not include the leading slash.
			if _, found := dict[strings.TrimPrefix(key, "/")]; found {
				return fmt.Errorf("dangerous pdf element detected: %s in object %d", key, objNr)
			}
		}
	}

	return nil
}