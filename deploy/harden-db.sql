REVOKE INSERT, UPDATE, DELETE ON TABLE schema_migrations FROM wastenot_app;
SELECT has_table_privilege('wastenot_app','schema_migrations','SELECT') AS can_read_migrations,
       has_table_privilege('wastenot_app','schema_migrations','UPDATE') AS can_modify_migrations;
