schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/master/schema/draft/2025-10/.schema.hcl"
id     = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/master/schema/draft/2025-10/.schema.hcl"

body {
    attribute "schema" {}
    attribute "id" {}
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