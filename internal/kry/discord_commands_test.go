package kry

import (
	"os"
	"path/filepath"
	"testing"
)

func copyDiscordPackageForCommands(t *testing.T) string {
	t.Helper()
	packageDir := filepath.Join("..", "..", "packages", "discord")
	fixtureDir := t.TempDir()
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatalf("list Discord package modules: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (filepath.Ext(name) != ".kry" && name != "kry.toml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(packageDir, name))
		if err != nil {
			t.Fatalf("read Discord package %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(fixtureDir, name), data, 0o600); err != nil {
			t.Fatalf("copy Discord package %s: %v", name, err)
		}
	}
	return fixtureDir
}

func runDiscordCommandFixture(t *testing.T, fixtureDir, source string) {
	t.Helper()
	fixturePath := filepath.Join(fixtureDir, "commands_test.kry")
	if err := os.WriteFile(fixturePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write Discord command test fixture: %v", err)
	}
	program, diagnostic := LoadProgram(fixturePath, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load Discord command test fixture: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("type-check Discord command test fixture: %s", diagnostic.Message)
	}
	runtime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatalf("create Discord command test runtime: %s", diagnostic.Message)
	}
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run Discord command test fixture: %s", diagnostic.Message)
	}
}

const discordCommandsFixture = `import "main"

fn expect_eq(label: String, actual: String, expected: String) -> Result[Nil, String] {
    if actual != expected { return err(label + " | got: " + actual + " | want: " + expected) }
    return ok(nil)
}

fn expect_true(label: String, condition: Bool) -> Result[Nil, String] {
    if !condition { return err(label + " | condition was false") }
    return ok(nil)
}

fn expect_error(label: String, value: Result[String, String]) -> Result[Nil, String] {
    match value {
        ok(_) => { return err(label + " | expected a failure") }
        err(_) => { return ok(nil) }
    }
}

fn expect_error_result(label: String, value: Result[Nil, String]) -> Result[Nil, String] {
    match value {
        ok(_) => { return err(label + " | expected a failure") }
        err(_) => { return ok(nil) }
    }
}

fn ping_command(context_json: String) -> String {
    let context: Context = result_unwrap(context_from_json(context_json))
    return "pong:" + context.resolved_command() + ":" + context.invoked_as()
}

fn count_command(context_json: String) -> String {
    let context: Context = result_unwrap(context_from_json(context_json))
    match context.converted_int("count") {
        ok(value) => { return "count:" + str(value) }
        err(problem) => { return "count-error:" + problem }
    }
}

fn target_command(context_json: String) -> String {
    let context: Context = result_unwrap(context_from_json(context_json))
    match context.converted_member("target") {
        ok(member) => { return "member:" + member.display_name() }
        err(problem) => { return "member-error:" + problem }
    }
}

fn role_command(context_json: String) -> String {
    let context: Context = result_unwrap(context_from_json(context_json))
    match context.converted_role("target") {
        ok(role) => { return "role:" + role.name }
        err(problem) => { return "role-error:" + problem }
    }
}

fn flag_command(context_json: String) -> String {
    let context: Context = result_unwrap(context_from_json(context_json))
    match context.converted_bool("flag") {
        ok(value) => { return "flag:" + str(value) }
        err(problem) => { return "flag-error:" + problem }
    }
}

fn ratio_command(context_json: String) -> String {
    let context: Context = result_unwrap(context_from_json(context_json))
    match context.converted_float("ratio") {
        ok(value) => { return "ratio:" + str(value) }
        err(problem) => { return "ratio-error:" + problem }
    }
}

fn args_command(context_json: String) -> String {
    let context: Context = result_unwrap(context_from_json(context_json))
    return array_join(context.parsed_arguments(), "|")
}

fn allow_check(context_json: String) -> String {
    return ""
}

fn deny_check(context_json: String) -> String {
    return "gate is closed"
}

fn command_error(context_json: String) -> String {
    let context: Json = result_unwrap(json_parse(context_json))
    let kind: String = result_unwrap(json_string(result_unwrap(json_object_get(context, "error"))))
    let message: String = result_unwrap(json_string(result_unwrap(json_object_get(context, "error_message"))))
    return kind + ":" + message
}

fn guild_message(content: String) -> Json {
    let quoted: String = result_unwrap(json_quote(content))
    let text: String = "{\"id\":\"500\",\"channel_id\":\"300\",\"guild_id\":\"100\",\"content\":" + quoted + ",\"author\":{\"id\":\"200\",\"username\":\"tester\"},\"member\":{\"user\":{\"id\":\"200\",\"username\":\"tester\"},\"roles\":[\"400\"]},\"mentions\":[]}"
    return result_unwrap(json_parse(text))
}

fn dm_message(content: String) -> Json {
    let quoted: String = result_unwrap(json_quote(content))
    let text: String = "{\"id\":\"501\",\"channel_id\":\"301\",\"content\":" + quoted + ",\"author\":{\"id\":\"200\",\"username\":\"tester\"},\"mentions\":[]}"
    return result_unwrap(json_parse(text))
}

fn main() -> Result[Nil, String] {
    let created: Bot = result_unwrap(bot("MTIzNDU2Nzg5MA==", ["Guilds"]))
    let client: Bot = created.with_prefixes(["!"])?

    expect_error("empty command name", command_spec("", "help text"))?
    expect_error("command name with space", command_spec("has space", "help text"))?
    expect_error("too many path segments", command_spec("a.b.c.d.e.f.g.h.i", "help text"))?
    expect_error("unknown converter", command_with_parameter(command_spec("valid_one", "help text")?, "count", "unknown"))?
    expect_error("zero cooldown rate", command_with_cooldown(command_spec("valid_two", "help text")?, 0, 5000, "author"))?
    expect_error("short cooldown period", command_with_cooldown(command_spec("valid_three", "help text")?, 1, 500, "author"))?
    expect_error("unknown cooldown scope", command_with_cooldown(command_spec("valid_four", "help text")?, 1, 5000, "everyone"))?
    expect_error("unknown permission", has_permissions_check(["NotAPermission"]))?
    expect_true("known permission", is_ok(has_permissions_check(["ban_members"])))?
    expect_true("manage_roles bit", is_ok(permission_bit("manage_roles")))?
    expect_eq("ban_members bit", str(result_unwrap(permission_bit("ban_members"))), "4")?

    let ping_spec: String = command_with_aliases(command_spec("ping", "Replies with Pong!")?, ["p"])?
    client.register_command(ping_spec, "ping_command")?
    expect_error_result("duplicate command", client.register_command(command_spec("ping", "Duplicate")?, "ping_command"))?
    expect_error_result("alias collision", client.register_command(command_with_aliases(command_spec("other", "x")?, ["p"])?, "ping_command"))?
    expect_error_result("missing handler", client.register_command(command_spec("no_handler", "x")?, ""))?
    expect_error_result("help with handler", client.register_command(help_command_spec("help_with_handler")?, "ping_command"))?

    let count_spec: String = command_with_parameter(command_spec("count", "Parses an integer.")?, "count", "int")?
    client.register_command(count_spec, "count_command")?

    let target_spec: String = command_with_parameter(command_spec("target", "Resolves a member.")?, "target", "member")?
    client.register_command(target_spec, "target_command")?

    let role_spec: String = command_with_parameter(command_spec("roleget", "Resolves a role.")?, "target", "role")?
    client.register_command(role_spec, "role_command")?

    let flag_spec: String = command_with_parameter(command_spec("flag", "Parses a boolean.")?, "flag", "bool")?
    client.register_command(flag_spec, "flag_command")?

    let ratio_spec: String = command_with_parameter(command_spec("ratio", "Parses a float.")?, "ratio", "float")?
    client.register_command(ratio_spec, "ratio_command")?

    let slow_spec: String = command_with_cooldown(command_spec("slow", "Rate limited.")?, 1, 60000, "author")?
    client.register_command(slow_spec, "ping_command")?

    let group_spec_text: String = group_spec("mod", "Moderation commands.", false)?
    client.register_command(group_spec_text, "")?

    let ban_spec: String = command_with_check(command_with_parameter(command_spec("mod.ban", "Bans a member.")?, "target", "member")?, "guild_only")?
    client.register_command(ban_spec, "ping_command")?

    let permissions_spec: String = command_with_check(command_spec("perms", "Needs ban members.")?, has_permissions_check(["ban_members"])?)?
    client.register_command(permissions_spec, "ping_command")?

    let kick_spec: String = command_with_check(command_spec("kickperm", "Needs kick members.")?, has_permissions_check(["kick_members"])?)?
    client.register_command(kick_spec, "ping_command")?

    let gated_spec: String = command_with_check(command_spec("gated", "Custom check that passes.")?, "custom_gate")?
    client.register_command(gated_spec, "ping_command")?

    let blocked_spec: String = command_with_check(command_spec("blocked", "Custom check that fails.")?, "custom_block")?
    client.register_command(blocked_spec, "ping_command")?

    let ghost_spec: String = command_with_check(command_spec("ghost", "Custom check without a handler.")?, "ghost_check")?
    client.register_command(ghost_spec, "ping_command")?

    client.register_command(help_command_spec("help")?, "")?

    let args_spec: String = command_spec("args", "Echoes parsed arguments.")?
    client.register_command(args_spec, "args_command")?

    poly_register("discord.command_check.custom_gate", "allow_check", 0)?
    poly_register("discord.command_check.custom_block", "deny_check", 0)?
    poly_register("discord.on_command_error", "command_error", 0)?

    let guild_json: String = "{\"id\":\"100\",\"owner_id\":\"999\",\"roles\":[{\"id\":\"100\",\"name\":\"@everyone\",\"permissions\":\"0\",\"position\":0,\"color\":0},{\"id\":\"400\",\"name\":\"Mod\",\"permissions\":\"4\",\"position\":1,\"color\":0}]}"
    client.cache_put("guild", "100", guild_json)?
    let member_json: String = "{\"user\":{\"id\":\"200\",\"username\":\"tester\"},\"roles\":[\"400\"],\"nick\":\"\"}"
    client.cache_put("member", "100:200", member_json)?
    let channel_json: String = "{\"id\":\"300\",\"name\":\"general\",\"type\":0,\"guild_id\":\"100\",\"permission_overwrites\":[]}"
    client.cache_put("channel", "300", channel_json)?
    let role_json: String = "{\"id\":\"400\",\"name\":\"Mod\",\"permissions\":\"4\",\"position\":1,\"color\":0}"
    client.cache_put("role", "100:400", role_json)?
    let user_json: String = "{\"id\":\"200\",\"username\":\"tester\"}"
    client.cache_put("user", "200", user_json)?

    poly_register("discord.on_prefix_command.legacy", "ping_command", 0)?

    expect_true("registered ping summary", contains(client.registered_commands_json(), "\"name\":\"ping\""))?
    expect_eq("plain command", discord_dispatch_prefix_command(client, guild_message("!ping"), ["!"])?, "pong:ping:ping")?
    expect_eq("alias resolves to canonical", discord_dispatch_prefix_command(client, guild_message("!p"), ["!"])?, "pong:ping:p")?
    expect_eq("unregistered command stays silent", discord_dispatch_prefix_command(client, guild_message("!nope"), ["!"])?, "")?
    expect_eq("legacy slot fallback", discord_dispatch_prefix_command(client, guild_message("!legacy"), ["!"])?, "pong:legacy:legacy")?
    expect_eq("int converter", discord_dispatch_prefix_command(client, guild_message("!count 42"), ["!"])?, "count:42")?
    expect_eq("bool converter", discord_dispatch_prefix_command(client, guild_message("!flag yes"), ["!"])?, "flag:true")?
    expect_eq("float converter", discord_dispatch_prefix_command(client, guild_message("!ratio 2.5"), ["!"])?, "ratio:2.5")?
    expect_eq("member converter", discord_dispatch_prefix_command(client, guild_message("!target <@200>"), ["!"])?, "member:tester")?
    expect_eq("role converter", discord_dispatch_prefix_command(client, guild_message("!roleget <@&400>"), ["!"])?, "role:Mod")?
    expect_eq("quoted parsed arguments", discord_dispatch_prefix_command(client, guild_message("!args \"hello world\" tail"), ["!"])?, "hello world|tail")?
    expect_eq("subcommand path", discord_dispatch_prefix_command(client, guild_message("!mod ban <@200>"), ["!"])?, "pong:mod.ban:mod")?
    expect_eq("has_permissions passes", discord_dispatch_prefix_command(client, guild_message("!perms"), ["!"])?, "pong:perms:perms")?
    expect_eq("custom check passes", discord_dispatch_prefix_command(client, guild_message("!gated"), ["!"])?, "pong:gated:gated")?

    let bad_integer: String = discord_dispatch_prefix_command(client, guild_message("!count nope"), ["!"])?
    expect_true("bad argument kind", contains(bad_integer, "bad_argument"))?
    expect_true("bad argument text", contains(bad_integer, "is not an integer"))?

    let missing_argument: String = discord_dispatch_prefix_command(client, guild_message("!count"), ["!"])?
    expect_true("missing argument kind", contains(missing_argument, "missing_argument"))?

    let too_many: String = discord_dispatch_prefix_command(client, guild_message("!count 1 2"), ["!"])?
    expect_true("too many arguments kind", contains(too_many, "too_many_arguments"))?

    let missing_subcommand: String = discord_dispatch_prefix_command(client, guild_message("!mod"), ["!"])?
    expect_true("missing subcommand kind", contains(missing_subcommand, "missing_subcommand"))?

    let unknown_subcommand: String = discord_dispatch_prefix_command(client, guild_message("!mod nuke"), ["!"])?
    expect_true("unknown subcommand kind", contains(unknown_subcommand, "command_not_found"))?
    expect_true("unknown subcommand text", contains(unknown_subcommand, "Unknown subcommand"))?

    let direct_message: String = discord_dispatch_prefix_command(client, dm_message("!mod ban <@200>"), ["!"])?
    expect_true("dm check failure kind", contains(direct_message, "check_failure"))?
    expect_true("dm check failure text", contains(direct_message, "server"))?

    let missing_permission: String = discord_dispatch_prefix_command(client, guild_message("!kickperm"), ["!"])?
    expect_true("permission check failure kind", contains(missing_permission, "check_failure"))?
    expect_true("permission check failure text", contains(missing_permission, "kick_members"))?

    let denied_check: String = discord_dispatch_prefix_command(client, guild_message("!blocked"), ["!"])?
    expect_true("custom check failure", contains(denied_check, "gate is closed"))?

    let ghost_check: String = discord_dispatch_prefix_command(client, guild_message("!ghost"), ["!"])?
    expect_true("unhandled custom check", contains(ghost_check, "no registered handler"))?

    expect_eq("first cooldown use", discord_dispatch_prefix_command(client, guild_message("!slow"), ["!"])?, "pong:slow:slow")?
    let second_slow: String = discord_dispatch_prefix_command(client, guild_message("!slow"), ["!"])?
    expect_true("cooldown rejection", contains(second_slow, "on cooldown"))?

    let help_list: String = discord_dispatch_prefix_command(client, guild_message("!help"), ["!"])?
    expect_true("help listing header", contains(help_list, "Commands:"))?
    expect_true("help listing entry", contains(help_list, "ping"))?

    let help_detail: String = discord_dispatch_prefix_command(client, guild_message("!help mod ban"), ["!"])?
    expect_true("help detail name", contains(help_detail, "Command: mod.ban"))?
    expect_true("help detail usage", contains(help_detail, "Usage: !mod ban <target>"))?
    expect_true("help detail checks", contains(help_detail, "Checks: guild_only"))?

    let help_alias: String = discord_dispatch_prefix_command(client, guild_message("!help p"), ["!"])?
    expect_true("help alias resolves", contains(help_alias, "Command: ping"))?
    expect_true("help alias listed", contains(help_alias, "Aliases: p"))?

    let help_unknown: String = discord_dispatch_prefix_command(client, guild_message("!help zzz"), ["!"])?
    expect_true("help unknown command", contains(help_unknown, "No such command"))?

    client.unregister_command("ping")?
    expect_error_result("second unregister fails", client.unregister_command("ping"))?
    expect_eq("legacy slot after unregister", discord_dispatch_prefix_command(client, guild_message("!ping"), ["!"])?, "pong:ping:ping")?
    expect_eq("alias removed with command", discord_dispatch_prefix_command(client, guild_message("!p"), ["!"])?, "")?
    return ok(nil)
}

result_unwrap(main())
`

func TestDiscordPrefixCommandFramework(t *testing.T) {
	fixtureDir := copyDiscordPackageForCommands(t)
	runDiscordCommandFixture(t, fixtureDir, discordCommandsFixture)
}
