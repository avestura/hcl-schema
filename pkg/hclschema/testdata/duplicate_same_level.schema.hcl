__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2025-10/.schema.hcl"
__id = "local://duplicate_same_level"

body {
    block_header "foo" {
        label_names = ["o"]
    }
    
    block_header "foo" {
        label_names = ["o2"]
    }
}
