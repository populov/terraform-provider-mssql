package mssql

import (
	"fmt"
	"log"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func DataSourceTables() *schema.Resource {
	return &schema.Resource{
		Read: ShowTables,
		Schema: map[string]*schema.Schema{
			"database": {
				Type:     schema.TypeString,
				Required: true,
			},
			"pattern": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"tables": {
				Type:     schema.TypeList,
				Computed: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},
		},
	}
}

func ShowTables(d *schema.ResourceData, meta interface{}) error {
	db, err := meta.(*MsSqlClient).GetDbConn()
	if err != nil {
		return err
	}

	database := d.Get("database").(string)
	pattern := d.Get("pattern").(string)

	stmtSQL := fmt.Sprintf("SHOW TABLES FROM %s", QuoteIdentifier(database))

	if pattern != "" {
		stmtSQL += fmt.Sprintf(" LIKE '%s'", pattern)
	}

	log.Printf("[DEBUG] SQL: %s", stmtSQL)

	rows, err := db.Query(stmtSQL)

	if err != nil {
		return err
	}

	defer rows.Close()

	var tables []string

	for rows.Next() {
		var table string

		err := rows.Scan(&table)

		if err != nil {
			return err
		}

		tables = append(tables, table)
	}

	err = d.Set("tables", tables)

	if err != nil {
		return err
	}

	d.SetId(resource.UniqueId())

	return nil
}
