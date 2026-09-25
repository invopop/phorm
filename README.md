# phorm

A Go HTTP client for [phorm](https://github.com/phax/phorm) — a standalone
business-document validation service built on
[phive](https://github.com/phax/phive) and
[phive-rules](https://github.com/phax/phive-rules) by
[@phax](https://github.com/phax).

This is the successor to the `invopop/phive` gRPC client, which is now archived.
Instead of running our own Java gRPC wrapper, we deploy phorm and talk to it over
its HTTP/JSON API. The package keeps the same request/response field names as the
old generated gRPC client, so callers migrate by swapping only the constructor.

## Install

```bash
go get github.com/invopop/phorm
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/invopop/phorm"
)

func main() {
	// baseURL of the phorm service and its X-Token auth value. An empty token
	// falls back to phorm.DefaultToken, so callers using phorm's stock token can
	// pass "".
	client := phorm.New("http://phorm:8080", os.Getenv("PHORM_TOKEN"))
	ctx := context.Background()

	// List validation rule sets (client-side filter).
	list, err := client.ListVesIds(ctx, &phorm.ListVesIdsRequest{Filter: "peppol"})
	if err != nil {
		log.Fatal(err)
	}
	for _, v := range list.Vesids {
		if v.Status == "VALID" {
			fmt.Printf("%s: %s\n", v.Vesid, v.Name)
		}
	}

	// Validate an XML document.
	xml, err := os.ReadFile("invoice.xml")
	if err != nil {
		log.Fatal(err)
	}
	result, err := client.ValidateXml(ctx, &phorm.ValidateXmlRequest{
		Vesid:      "eu.peppol.bis3:invoice:2024.5",
		XmlContent: xml,
	})
	if err != nil {
		log.Fatal(err) // the validation never ran; see "Errors vs. findings"
	}
	fmt.Printf("Valid: %v\n", result.Success)
	if !result.Success {
		fmt.Println(result.Report())
	}
}
```

### Errors vs. findings

Validation findings live in `result.Results[].Errors` / `.Warnings`, and
`result.Success` reports the outcome.

A non-nil error means no validation happened at all: the service was
unreachable, the `X-Token` was rejected (403), the VESID could not be resolved,
or the body was not readable as XML.

A document that simply breaks a rule is **not** an error. Note that phorm
answers one with HTTP 400 and the report as the body, which it also uses for a
rejected request, so the status alone cannot tell the two apart — the client
separates them by whether the body is a validation report, and returns the
report either way.

### Rendering a report

`result.Report()` turns the findings into one human readable block, ready to put
in an error message. It works for any VESID:

```
validation failed: 1 error against ro.gov.mfinante.cius-ro:ubl-invoice:1.0.9

schematron-xslt2 (ROeFactura-UBL-validation-Invoice_v1.0.9.xslt):
  1) ERROR [BR-RO-110] If the Seller's country Code (BT-40) is RO, then ...
     at /:Invoice[1]
```

- Errors come before warnings, grouped by validation layer, and artifact paths
  are cut down to the file name.
- The rule id a message repeats (`[BR-01]-…`, `BR-01: …`) is dropped, since it
  is already shown in brackets.
- At most 25 problems are listed; the header still counts all of them.
- A passing result reads `validation passed: …`, with any warnings listed.

## Running phorm

phorm is a Java service. Run the upstream image directly:

```bash
docker run -d --name phorm -p 8080:8080 phelger/phorm
```

Use `phelger/phorm-arm64` on arm64 hosts such as Apple Silicon. The image is
published as `phelger/phorm`; there is no `phax/phorm`.

It takes a few seconds to start. It is ready once this returns HTTP 200:

```bash
curl -s -o /dev/null -w '%{http_code}\n' \
  -H 'X-Token: phorm-dev-token' \
  'http://localhost:8080/api/get/vesids?include-deprecated=true'
```

To have failed validations answered with 200 instead of 400:

```bash
docker run -d --name phorm -p 8080:8080 -e PHORM_API_RESPONSE_ONFAILURE_HTTP400=false phax/phorm
```

Key phorm settings:

- `phorm.api.requiredtoken` — the `X-Token` the client must send. Defaults to
  `phorm-dev-token`, which is also this package's `DefaultToken`.
- `phorm.api.response.onfailure.http400` — whether a failed validation is
  answered with 400 (default `true`) or 200.
- `webapp.datapath` — configuration and data location, `/config/phorm` in the
  image. Mount it to keep settings across restarts.

## Migration from the phive gRPC client

| gRPC (old) | phorm HTTP (new) |
|---|---|
| `phive.NewValidationServiceClient(conn)` | `phorm.New(baseURL, token)` |
| `ValidateXml(ctx, req)` | `POST /api/validate/{vesid}`, raw XML body, `X-Token` header |
| `ListVesIds(ctx, req)` | `GET /api/get/vesids?include-deprecated=true` (filter applied client-side) |
| `PHIVE_ADDRESS=phive:50051` | `PHORM_URL=http://phorm:8080` + `PHORM_TOKEN=…` |

Response field names (`Success`, `Results`, `ValidationType`, `ArtifactId`,
`Errors`, `Warnings`, `Level`, `Message`, `Location`, `TestId`, `ResolvedVesid`)
are unchanged, so `ProcessValidationResponse`-style code keeps working.

One behavioural difference is worth checking at each call site: the gRPC client
reported an unreachable service and a failed validation the same way, through
the response. Here an unreachable service is an error and a failed validation is
not, so a caller that skips validation when the request errors no longer skips
it for documents that are merely invalid.

**Note:** many rule sets are deprecated; filter by `Status == "VALID"` for
current ones.

## License

Apache License 2.0

## Links

- [phorm](https://github.com/phax/phorm) — validation service
- [phive](https://github.com/phax/phive) — core validation engine
- [phive-rules](https://github.com/phax/phive-rules) — pre-built validation rules
