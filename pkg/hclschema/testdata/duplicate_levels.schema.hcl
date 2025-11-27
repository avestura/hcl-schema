__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/master/schema/draft/2025-10/.schema.hcl"
__id = "local://duplicate_levels"

body {
    block_header "foo" {
        label_names = ["o"]

        body {
            block_header "foo" {
                label_names = ["i"]
            }
        }
    }
}
