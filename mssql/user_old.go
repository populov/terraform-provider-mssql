package mssql

import (
	"context"
	"database/sql"
	"github.com/saritasa/terraform-provider-mssql/model"
	"strings"
)

func (c *Connector) GetUserOld(ctx context.Context, database, username string) (*model.User, error) {
	cmd := `DECLARE @stmt nvarchar(max)
          IF @@VERSION LIKE 'Microsoft SQL Azure%'
            BEGIN
              SET @stmt = 'WITH CTE_Roles (principal_id, role_principal_id) AS ' +
                          '(' +
                          '  SELECT member_principal_id, role_principal_id FROM [sys].[database_role_members] WHERE member_principal_id = DATABASE_PRINCIPAL_ID(' + QuoteName(@username, '''') + ')' +
                          '  UNION ALL ' +
                          '  SELECT member_principal_id, drm.role_principal_id FROM [sys].[database_role_members] drm' +
                          '    INNER JOIN CTE_Roles cr ON drm.member_principal_id = cr.role_principal_id' +
                          ') ' +
                          'SELECT p.principal_id, p.name, p.authentication_type_desc, COALESCE(p.default_schema_name, ''''), COALESCE(p.default_language_name, ''''), p.sid, '''', COALESCE(STRING_AGG(USER_NAME(r.role_principal_id), '',''), '''') ' +
                          'FROM [sys].[database_principals] p' +
                          '  LEFT JOIN CTE_Roles r ON p.principal_id = r.principal_id ' +
                          'WHERE p.name = ' + QuoteName(@username, '''') + ' ' +
                          'GROUP BY p.principal_id, p.name, p.authentication_type_desc, p.default_schema_name, p.default_language_name, p.sid'
            END
          ELSE
            BEGIN
              SET @stmt = 'WITH CTE_Roles (principal_id, role_principal_id) AS ' +
                          '(' +
                          '  SELECT member_principal_id, role_principal_id FROM ' + QuoteName(@database) + '.[sys].[database_role_members] WHERE member_principal_id = DATABASE_PRINCIPAL_ID(' + QuoteName(@username, '''') + ')' +
                          '  UNION ALL ' +
                          '  SELECT member_principal_id, drm.role_principal_id FROM ' + QuoteName(@database) + '.[sys].[database_role_members] drm' +
                          '    INNER JOIN CTE_Roles cr ON drm.member_principal_id = cr.role_principal_id' +
                          ') ' +
                          'SELECT p.principal_id, p.name, p.authentication_type_desc, COALESCE(p.default_schema_name, ''''), COALESCE(p.default_language_name, ''''), p.sid, COALESCE(sl.name, ''''), COALESCE(STRING_AGG(USER_NAME(r.role_principal_id), '',''), '''') ' +
                          'FROM ' + QuoteName(@database) + '.[sys].[database_principals] p' +
                          '  LEFT JOIN CTE_Roles r ON p.principal_id = r.principal_id ' +
                          '  LEFT JOIN [master].[sys].[sql_logins] sl ON p.sid = sl.sid ' +
                          'WHERE p.name = ' + QuoteName(@username, '''') + ' ' +
                          'GROUP BY p.principal_id, p.name, p.authentication_type_desc, p.default_schema_name, p.default_language_name, p.sid, sl.name'
            END
          EXEC (@stmt)`
	var (
		user  model.User
		sid   []byte
		roles string
	)
	var schema, language string
	err := c.
		setDatabase(database).
		QueryRowContext(ctx, cmd,
			func(r *sql.Row) error {
				return r.Scan(&user.PrincipalID, &user.Username, &user.AuthType, &schema, &language, &sid, &user.LoginName, &roles)
			},
			sql.Named("database", database),
			sql.Named("username", username),
		)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if user.AuthType == "INSTANCE" && user.LoginName == "" {
		cmd = "SELECT name FROM [sys].[sql_logins] WHERE sid = @sid"
		c.Database = "master"
		err = c.QueryRowContext(ctx, cmd,
			func(r *sql.Row) error {
				return r.Scan(&user.LoginName)
			},
			sql.Named("sid", sid),
		)
		if err != nil {
			return nil, err
		}
	}
	if roles == "" {
		user.Roles = make([]string, 0)
	} else {
		user.Roles = strings.Split(roles, ",")
	}
	return &user, nil
}

func (c *Connector) CreateUserOld(ctx context.Context, database string, user *model.User) error {
	cmd := `DECLARE @stmt nvarchar(max)
          DECLARE @language nvarchar(max) = @defaultLanguage
          IF @language = '' SET @language = NULL
          IF @authType = 'INSTANCE'
            BEGIN
              SET @stmt = 'CREATE USER ' + QuoteName(@username) + ' FOR LOGIN ' + QuoteName(@loginName) + ' ' +
                          'WITH DEFAULT_SCHEMA = ' + QuoteName(@defaultSchema)
            END
          IF @authType = 'DATABASE'
            BEGIN
              SET @stmt = 'CREATE USER ' + QuoteName(@username) + ' WITH PASSWORD = ' + QuoteName(@password, '''') + ', ' +
                          'DEFAULT_SCHEMA = ' + QuoteName(@defaultSchema)
              IF NOT @@VERSION LIKE 'Microsoft SQL Azure%'
                BEGIN
                  SET @stmt = @stmt + ', DEFAULT_LANGUAGE = ' + Coalesce(QuoteName(@language), 'NONE')
                END
            END
          IF @authType = 'EXTERNAL'
            BEGIN
              IF @@VERSION LIKE 'Microsoft SQL Azure%'
                BEGIN
                  IF @objectId != ''
                    BEGIN
                      SET @stmt = 'CREATE USER ' + QuoteName(@username) + ' WITH SID=' + CONVERT(varchar(64), CAST(CAST(@objectId AS UNIQUEIDENTIFIER) AS VARBINARY(16)), 1) + ', TYPE=E'
                    END
                  ELSE
                    BEGIN
                      SET @stmt = 'CREATE USER ' + QuoteName(@username) + ' FROM EXTERNAL PROVIDER'
                  END
                END
              ELSE
                BEGIN
                  SET @stmt = 'CREATE USER ' + QuoteName(@username) + ' FOR LOGIN ' + QuoteName(@username) + ' FROM EXTERNAL PROVIDER ' +
                              'WITH DEFAULT_SCHEMA = ' + QuoteName(@defaultSchema) + ', ' +
                              'DEFAULT_LANGUAGE = ' + Coalesce(QuoteName(@language), 'NONE')
                END
            END
          IF exists (select compatibility_level FROM sys.databases where name = db_name() and compatibility_level < 130)
          BEGIN
              DECLARE @mssql NVARCHAR(MAX);
              SET @mssql = N'Create FUNCTION [dbo].[String_Split]
                    (
                        @string    nvarchar(max),
                        @delimiter nvarchar(max)
                    )
                    /*
                        The same as STRING_SPLIT for compatibility level < 130
                        https://docs.microsoft.com/en-us/mssql/t-mssql/functions/string-split-transact-mssql?view=mssql-server-ver15
                    */
                    RETURNS TABLE AS RETURN
                    (
                        SELECT 
                          --ROW_NUMBER ( ) over(order by (select 0))                            AS id     --  intuitive, but not correect
                            Split.a.value(''let $n := . return count(../*[. << $n]) + 1'', ''int'') AS id
                          , Split.a.value(''.'', ''NVARCHAR(MAX)'')                                 AS value
                        FROM
                        (
                            SELECT CAST(''<X>''+REPLACE(@string, @delimiter, ''</X><X>'')+''</X>'' AS XML) AS String
                        ) AS a
                        CROSS APPLY String.nodes(''/X'') AS Split(a)
                    )';
              EXEC sp_executesql @mssql;
          END
          SET @stmt = @stmt + '; ' +
                      'DECLARE role_cur CURSOR FOR SELECT name FROM ' + QuoteName(@database) + '.[sys].[database_principals] WHERE type = ''R'' AND name != ''public'' AND name COLLATE SQL_Latin1_General_CP1_CI_AS IN (SELECT value FROM String_Split(' + QuoteName(@roles, '''') + ', '',''));' +
                      'DECLARE @role nvarchar(max);' +
                      'OPEN role_cur;' +
                      'FETCH NEXT FROM role_cur INTO @role;' +
                      'WHILE @@FETCH_STATUS = 0' +
                      '  BEGIN' +
                      '    DECLARE @mssql nvarchar(max);' +
                      '    SET @mssql = ''ALTER ROLE '' + QuoteName(@role) + '' ADD MEMBER ' + QuoteName(@username) + ''';' +
                      '    EXEC (@mssql);' +
                      '    FETCH NEXT FROM role_cur INTO @role;' +
                      '  END;' +
                      'CLOSE role_cur;' +
                      'DEALLOCATE role_cur;'
          EXEC (@stmt)`
	return c.
		setDatabase(user.Database).
		ExecContext(ctx, cmd,
			sql.Named("database", user.Database),
			sql.Named("username", user.Username),
			sql.Named("objectId", user.ObjectId),
			sql.Named("loginName", user.LoginName),
			sql.Named("password", user.Password),
			sql.Named("authType", user.AuthType),
			sql.Named("defaultSchema", "dbo"),
			sql.Named("defaultLanguage", "NONE"),
			sql.Named("roles", strings.Join(user.Roles, ",")),
		)
}

func (c *Connector) UpdateUser(ctx context.Context, database string, user *model.User) error {
	cmd := `DECLARE @stmt nvarchar(max)
          SET @stmt = 'ALTER USER ' + QuoteName(@username) + ' '
          DECLARE @language nvarchar(max) = @defaultLanguage
          IF @language = '' SET @language = NULL
          SET @stmt = @stmt + 'WITH DEFAULT_SCHEMA = ' + QuoteName(@defaultSchema)
          DECLARE @auth_type nvarchar(max) = (SELECT authentication_type_desc FROM [sys].[database_principals] WHERE name = @username)
          IF NOT @@VERSION LIKE 'Microsoft SQL Azure%' AND @auth_type != 'INSTANCE'
            BEGIN
              SET @stmt = @stmt + ', DEFAULT_LANGUAGE = ' + Coalesce(QuoteName(@language), 'NONE')
            END
          IF exists (select compatibility_level FROM sys.databases where name = db_name() and compatibility_level < 130)
          BEGIN
              DECLARE @mssql NVARCHAR(MAX);
              SET @mssql = N'Create FUNCTION [dbo].[String_Split]
                    (
                        @string    nvarchar(max),
                        @delimiter nvarchar(max)
                    )
                    /*
                        The same as STRING_SPLIT for compatibility level < 130
                        https://docs.microsoft.com/en-us/mssql/t-mssql/functions/string-split-transact-mssql?view=mssql-server-ver15
                    */
                    RETURNS TABLE AS RETURN
                    (
                        SELECT 
                          --ROW_NUMBER ( ) over(order by (select 0))                            AS id     --  intuitive, but not correect
                            Split.a.value(''let $n := . return count(../*[. << $n]) + 1'', ''int'') AS id
                          , Split.a.value(''.'', ''NVARCHAR(MAX)'')                                 AS value
                        FROM
                        (
                            SELECT CAST(''<X>''+REPLACE(@string, @delimiter, ''</X><X>'')+''</X>'' AS XML) AS String
                        ) AS a
                        CROSS APPLY String.nodes(''/X'') AS Split(a)
                    )';
              EXEC sp_executesql @mssql;
          END
          SET @stmt = @stmt + '; ' +
                      'DECLARE @mssql nvarchar(max);' +
                      'DECLARE @role nvarchar(max);' +
                      'DECLARE del_role_cur CURSOR FOR SELECT name FROM ' + QuoteName(@database) + '.[sys].[database_principals] WHERE type = ''R'' AND name != ''public'' AND name IN (SELECT name FROM ' + QuoteName(@database) + '.[sys].[database_role_members] drm, ' + QuoteName(@database) + '.[sys].[database_principals] db WHERE drm.member_principal_id = DATABASE_PRINCIPAL_ID(' + QuoteName(@username, '''') + ') AND drm.role_principal_id = db.principal_id) AND name COLLATE SQL_Latin1_General_CP1_CI_AS NOT IN(SELECT value FROM String_Split(' + QuoteName(@roles, '''') + ', '',''));' +
                      'DECLARE add_role_cur CURSOR FOR SELECT name FROM ' + QuoteName(@database) + '.[sys].[database_principals] WHERE type = ''R'' AND name != ''public'' AND name NOT IN (SELECT name FROM ' + QuoteName(@database) + '.[sys].[database_role_members] drm, ' + QuoteName(@database) + '.[sys].[database_principals] db WHERE drm.member_principal_id = DATABASE_PRINCIPAL_ID(' + QuoteName(@username, '''') + ') AND drm.role_principal_id = db.principal_id) AND name COLLATE SQL_Latin1_General_CP1_CI_AS IN(SELECT value FROM String_Split(' + QuoteName(@roles, '''') + ', '',''));' +
                      'OPEN del_role_cur;' +
                      'FETCH NEXT FROM del_role_cur INTO @role;' +
                      'WHILE @@FETCH_STATUS = 0' +
                      '  BEGIN' +
                      '    SET @mssql = ''ALTER ROLE '' + QuoteName(@role) + '' DROP MEMBER ' + QuoteName(@username) + ''';' +
                      '    EXEC (@mssql);' +
                      '    FETCH NEXT FROM del_role_cur INTO @role;' +
                      '  END;' +
                      'CLOSE del_role_cur;' +
                      'DEALLOCATE del_role_cur;' +
                      'OPEN add_role_cur;' +
                      'FETCH NEXT FROM add_role_cur INTO @role;' +
                      'WHILE @@FETCH_STATUS = 0' +
                      '  BEGIN' +
                      '    SET @mssql = ''ALTER ROLE '' + QuoteName(@role) + '' ADD MEMBER ' + QuoteName(@username) + ''';' +
                      '    EXEC (@mssql);' +
                      '    FETCH NEXT FROM add_role_cur INTO @role;' +
                      '  END;' +
                      'CLOSE add_role_cur;' +
                      'DEALLOCATE add_role_cur;'
          EXEC (@stmt)`
	return c.
		setDatabase(user.Database).
		ExecContext(ctx, cmd,
			sql.Named("database", user.Database),
			sql.Named("username", user.Username),
			sql.Named("defaultSchema", "dbo"),
			sql.Named("defaultLanguage", "NONE"),
			sql.Named("roles", strings.Join(user.Roles, ",")),
		)
}
