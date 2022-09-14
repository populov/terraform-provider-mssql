# :point_right: Moved to [saritasa-nest/terraform-provider-mssql](https://github.com/saritasa-nest/terraform-provider-mssql)

# Microsoft SQL Server Terraform Provider

## Usage

```hcl
terraform {
  required_providers {
    mssql = {
      source  = "saritasa/provider"
      version = "~> 0.1.0"
    }
  }
  required_version = ">= 0.13"
}

provider "mssql" {
  endpoint = "localhost"
  username = "admin"
  password = "mypass"
}
```
