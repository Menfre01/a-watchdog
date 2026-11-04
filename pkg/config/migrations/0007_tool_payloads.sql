-- 0007_tool_payloads.sql -- 工具入参与结果(设计依据:openspec/changes/tool-payload-capture)。
-- 单字段上限 256 KB;超限只存前缀并标记截断。
ALTER TABLE tool_events ADD COLUMN input_gzip BLOB;
ALTER TABLE tool_events ADD COLUMN input_truncated INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tool_events ADD COLUMN result_gzip BLOB;
ALTER TABLE tool_events ADD COLUMN result_truncated INTEGER NOT NULL DEFAULT 0;
