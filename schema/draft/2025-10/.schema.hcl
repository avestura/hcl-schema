// The hcl-schema meta-schema, draft 2025-10.
//
// This is the original draft and is frozen: documents that pin it keep their
// original meaning. `attribute` takes only `required`; `block_header` takes
// only `label_names`, `id` and `ref`; a `block_header` with no `body` accepts
// arbitrary content; and a name declared twice in one body resolves to the
// first declaration.
//
// New work should pin draft 2026-09, which adds the type system and tightens
// those last two rules. See schema/draft/2026-09/.schema.hcl.

__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2025-10/.schema.hcl"
__id     = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2025-10/.schema.hcl"

body {
    attribute "__schema" {}
    attribute "__id" {}
    block_header "body" {
        id = "bodyRef"
        body {
            block_header "block_header" {
                label_names = ["block_header_type"]
                body {
                    attribute "label_names" {
                        required = false
                    }
                    attribute "ref" {
                        required = false
                    }
                    attribute "id" {
                        required = false
                    }

                    block_header "body" {
                        ref = block_header.bodyRef
                    }
                }
            }
            block_header "attribute" {
                label_names = ["attribute_name"]
                body {
                    attribute "required" {
                        required = false
                    }
                }
            }
        }
    }
}