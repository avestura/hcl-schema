__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/master/schema/draft/2025-10/.schema.hcl"
id     = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/master/example.schema.hcl"

body {
    attribute "myattr" {
        required = true
    }

    block_header "tag" {
        label_names = ["name1"]

        body { 
            attribute "x" {}
        }
    }
}

// Defines an HCL file like this:
//
// myattr = "x"
// tag "name" {
//     x = 2
// }
// tag "name2" {}
//