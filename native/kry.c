#define _POSIX_C_SOURCE 200809L

#include <ctype.h>
#include <errno.h>
#include <inttypes.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/*
 * Kryndel native runtime
 *
 * This file intentionally has no dependency on the Python bootstrap or on a
 * third-party runtime.  It is a small, native, tree-walk implementation of
 * the productive core language.  The bootstrap remains available for the
 * historical bytecode contracts; this executable is the independent route.
 */

typedef struct {
    const char *name;
    const char *source;
    size_t length;
    int line;
    int column;
    size_t offset;
} Source;

typedef enum {
    TOK_EOF,
    TOK_ID,
    TOK_INT,
    TOK_FLOAT,
    TOK_STRING,
    TOK_FN,
    TOK_LET,
    TOK_MUT,
    TOK_IF,
    TOK_ELSE,
    TOK_WHILE,
    TOK_RETURN,
    TOK_BREAK,
    TOK_CONTINUE,
    TOK_TRUE,
    TOK_FALSE,
    TOK_NIL,
    TOK_PUB,
    TOK_IMPORT,
    TOK_STRUCT,
    TOK_ENUM,
    TOK_MATCH,
    TOK_LPAREN,
    TOK_RPAREN,
    TOK_LBRACE,
    TOK_RBRACE,
    TOK_LBRACKET,
    TOK_RBRACKET,
    TOK_COMMA,
    TOK_COLON,
    TOK_SEMICOLON,
    TOK_ARROW,
    TOK_PLUS,
    TOK_MINUS,
    TOK_STAR,
    TOK_SLASH,
    TOK_PERCENT,
    TOK_BANG,
    TOK_EQUAL,
    TOK_EQUAL_EQUAL,
    TOK_BANG_EQUAL,
    TOK_LESS,
    TOK_LESS_EQUAL,
    TOK_GREATER,
    TOK_GREATER_EQUAL,
    TOK_AND_AND,
    TOK_OR_OR,
    TOK_DOT
} TokenKind;

typedef struct {
    TokenKind kind;
    size_t start;
    size_t length;
    int line;
    int column;
} Token;

typedef struct {
    Token *items;
    size_t count;
    size_t capacity;
    Source source;
    char error[512];
    int error_line;
    int error_column;
    int line;
    int column;
} Lexer;

typedef struct Expr Expr;
typedef struct Stmt Stmt;
typedef struct Function Function;

typedef enum {
    EXPR_INT,
    EXPR_FLOAT,
    EXPR_BOOL,
    EXPR_NIL,
    EXPR_STRING,
    EXPR_VARIABLE,
    EXPR_UNARY,
    EXPR_BINARY,
    EXPR_CALL,
    EXPR_ARRAY,
    EXPR_INDEX
} ExprKind;

struct Expr {
    ExprKind kind;
    int line;
    int column;
    union {
        int64_t int_value;
        double float_value;
        int bool_value;
        char *string_value;
        char *variable;
        struct { int op; Expr *operand; } unary;
        struct { int op; Expr *left; Expr *right; } binary;
        struct { char *name; Expr **args; size_t count; } call;
        struct { Expr **items; size_t count; } array;
        struct { Expr *base; Expr *index; } index;
    } as;
};

typedef enum {
    STMT_LET,
    STMT_EXPR,
    STMT_ASSIGN,
    STMT_IF,
    STMT_WHILE,
    STMT_RETURN,
    STMT_BREAK,
    STMT_CONTINUE
} StmtKind;

struct Stmt {
    StmtKind kind;
    int line;
    int column;
    union {
        struct { char *name; Expr *initializer; } let;
        Expr *expression;
        struct { char *name; Expr *value; } assign;
        struct { Expr *condition; Stmt **then_body; size_t then_count; Stmt **else_body; size_t else_count; } if_stmt;
        struct { Expr *condition; Stmt **body; size_t count; } while_stmt;
        Expr *return_value;
    } as;
};

typedef struct {
    char *name;
} Parameter;

struct Function {
    char *name;
    Parameter *parameters;
    size_t parameter_count;
    Stmt **body;
    size_t body_count;
};

typedef struct {
    Stmt **statements;
    size_t statement_count;
    Function **functions;
    size_t function_count;
} Program;

static void *xmalloc(size_t size) {
    void *result = malloc(size == 0 ? 1 : size);
    if (!result) {
        fprintf(stderr, "kry: fatal: out of memory\n");
        exit(2);
    }
    return result;
}

static void *xrealloc(void *ptr, size_t size) {
    void *result = realloc(ptr, size == 0 ? 1 : size);
    if (!result) {
        fprintf(stderr, "kry: fatal: out of memory\n");
        exit(2);
    }
    return result;
}

static char *xstrndup0(const char *text, size_t length) {
    char *result = xmalloc(length + 1);
    memcpy(result, text, length);
    result[length] = '\0';
    return result;
}

static char *xstrdup0(const char *text) {
    return xstrndup0(text, strlen(text));
}

static bool is_identifier_start(unsigned char c) {
    return isalpha(c) || c == '_';
}

static bool is_identifier_continue(unsigned char c) {
    return isalnum(c) || c == '_';
}

static void lexer_push(Lexer *lexer, TokenKind kind, size_t start, size_t length, int line, int column) {
    if (lexer->count == lexer->capacity) {
        lexer->capacity = lexer->capacity == 0 ? 64 : lexer->capacity * 2;
        lexer->items = xrealloc(lexer->items, lexer->capacity * sizeof(Token));
    }
    lexer->items[lexer->count++] = (Token){kind, start, length, line, column};
}

static bool slice_equals(Source source, Token token, const char *literal) {
    size_t length = strlen(literal);
    return token.length == length && memcmp(source.source + token.start, literal, length) == 0;
}

static TokenKind keyword_kind(Source source, Token token) {
    static const struct { const char *word; TokenKind kind; } keywords[] = {
        {"fn", TOK_FN}, {"let", TOK_LET}, {"mut", TOK_MUT}, {"if", TOK_IF},
        {"else", TOK_ELSE}, {"while", TOK_WHILE}, {"return", TOK_RETURN},
        {"break", TOK_BREAK}, {"continue", TOK_CONTINUE}, {"true", TOK_TRUE},
        {"false", TOK_FALSE}, {"nil", TOK_NIL}, {"pub", TOK_PUB},
        {"import", TOK_IMPORT}, {"struct", TOK_STRUCT}, {"enum", TOK_ENUM},
        {"match", TOK_MATCH}
    };
    for (size_t i = 0; i < sizeof(keywords) / sizeof(keywords[0]); i++) {
        if (slice_equals(source, token, keywords[i].word)) return keywords[i].kind;
    }
    return TOK_ID;
}

static void lexer_fail(Lexer *lexer, const char *format, ...) {
    if (lexer->error[0] != '\0') return;
    va_list args;
    va_start(args, format);
    vsnprintf(lexer->error, sizeof(lexer->error), format, args);
    va_end(args);
    lexer->error_line = lexer->line;
    lexer->error_column = lexer->column;
}

static void lex_source(Lexer *lexer) {
    size_t i = 0;
    lexer->line = 1;
    lexer->column = 1;
    while (i < lexer->source.length) {
        unsigned char c = (unsigned char)lexer->source.source[i];
        if (c == ' ' || c == '\t' || c == '\r') { i++; lexer->column++; continue; }
        if (c == '\n') { i++; lexer->line++; lexer->column = 1; continue; }
        if (c == '/' && i + 1 < lexer->source.length && lexer->source.source[i + 1] == '/') {
            i += 2; lexer->column += 2;
            while (i < lexer->source.length && lexer->source.source[i] != '\n') { i++; lexer->column++; }
            continue;
        }
        if (c == '/' && i + 1 < lexer->source.length && lexer->source.source[i + 1] == '*') {
            int depth = 1;
            i += 2; lexer->column += 2;
            while (i < lexer->source.length && depth > 0) {
                if (i + 1 < lexer->source.length && lexer->source.source[i] == '/' && lexer->source.source[i + 1] == '*') {
                    depth++; i += 2; lexer->column += 2;
                } else if (i + 1 < lexer->source.length && lexer->source.source[i] == '*' && lexer->source.source[i + 1] == '/') {
                    depth--; i += 2; lexer->column += 2;
                } else if (lexer->source.source[i] == '\n') {
                    i++; lexer->line++; lexer->column = 1;
                } else { i++; lexer->column++; }
            }
            if (depth != 0) lexer_fail(lexer, "unterminated block comment");
            continue;
        }
        int line = lexer->line;
        int column = lexer->column;
        if (is_identifier_start(c)) {
            size_t start = i++;
            while (i < lexer->source.length && is_identifier_continue((unsigned char)lexer->source.source[i])) { i++; lexer->column++; }
            Token raw = {TOK_ID, start, i - start, line, column};
            raw.kind = keyword_kind(lexer->source, raw);
            lexer_push(lexer, raw.kind, raw.start, raw.length, raw.line, raw.column);
            lexer->column = column + (int)(i - start);
            continue;
        }
        if (isdigit(c)) {
            size_t start = i++;
            while (i < lexer->source.length && isdigit((unsigned char)lexer->source.source[i])) { i++; lexer->column++; }
            TokenKind kind = TOK_INT;
            if (i + 1 < lexer->source.length && lexer->source.source[i] == '.' && isdigit((unsigned char)lexer->source.source[i + 1])) {
                kind = TOK_FLOAT; i++; lexer->column++;
                while (i < lexer->source.length && isdigit((unsigned char)lexer->source.source[i])) { i++; lexer->column++; }
            }
            lexer_push(lexer, kind, start, i - start, line, column);
            lexer->column = column + (int)(i - start);
            continue;
        }
        if (c == '"') {
            size_t start = ++i;
            lexer->column++;
            bool closed = false;
            while (i < lexer->source.length) {
                unsigned char current = (unsigned char)lexer->source.source[i];
                if (current == '\\') {
                    if (i + 1 >= lexer->source.length) break;
                    i += 2; lexer->column += 2;
                } else if (current == '"') {
                    closed = true; break;
                } else if (current == '\n') {
                    lexer_fail(lexer, "newline in string literal");
                    break;
                } else { i++; lexer->column++; }
            }
            if (!closed) { lexer_fail(lexer, "unterminated string literal"); break; }
            lexer_push(lexer, TOK_STRING, start, i - start, line, column);
            i++; lexer->column++;
            continue;
        }
        TokenKind kind = TOK_EOF;
        size_t width = 1;
        switch (c) {
            case '(': kind = TOK_LPAREN; break; case ')': kind = TOK_RPAREN; break;
            case '{': kind = TOK_LBRACE; break; case '}': kind = TOK_RBRACE; break;
            case '[': kind = TOK_LBRACKET; break; case ']': kind = TOK_RBRACKET; break;
            case ',': kind = TOK_COMMA; break; case ':': kind = TOK_COLON; break;
            case ';': kind = TOK_SEMICOLON; break; case '+': kind = TOK_PLUS; break;
            case '-':
                if (i + 1 < lexer->source.length && lexer->source.source[i + 1] == '>') { kind = TOK_ARROW; width = 2; }
                else kind = TOK_MINUS;
                break;
            case '*': kind = TOK_STAR; break; case '%': kind = TOK_PERCENT; break;
            case '!':
                if (i + 1 < lexer->source.length && lexer->source.source[i + 1] == '=') { kind = TOK_BANG_EQUAL; width = 2; }
                else kind = TOK_BANG;
                break;
            case '=':
                if (i + 1 < lexer->source.length && lexer->source.source[i + 1] == '=') { kind = TOK_EQUAL_EQUAL; width = 2; }
                else kind = TOK_EQUAL;
                break;
            case '<':
                if (i + 1 < lexer->source.length && lexer->source.source[i + 1] == '=') { kind = TOK_LESS_EQUAL; width = 2; }
                else kind = TOK_LESS;
                break;
            case '>':
                if (i + 1 < lexer->source.length && lexer->source.source[i + 1] == '=') { kind = TOK_GREATER_EQUAL; width = 2; }
                else kind = TOK_GREATER;
                break;
            case '&':
                if (i + 1 < lexer->source.length && lexer->source.source[i + 1] == '&') { kind = TOK_AND_AND; width = 2; }
                else lexer_fail(lexer, "expected '&' in '&&'");
                break;
            case '|':
                if (i + 1 < lexer->source.length && lexer->source.source[i + 1] == '|') { kind = TOK_OR_OR; width = 2; }
                else lexer_fail(lexer, "expected '|' in '||'");
                break;
            case '/': kind = TOK_SLASH; break; case '.': kind = TOK_DOT; break;
            default: lexer_fail(lexer, "unexpected character '%c'", c); break;
        }
        if (lexer->error[0] != '\0') break;
        lexer_push(lexer, kind, i, width, line, column);
        i += width; lexer->column += (int)width;
    }
    lexer_push(lexer, TOK_EOF, lexer->source.length, 0, lexer->line, lexer->column);
}

static Expr *new_expr(ExprKind kind, Token token) {
    Expr *expr = xmalloc(sizeof(Expr));
    memset(expr, 0, sizeof(*expr));
    expr->kind = kind; expr->line = token.line; expr->column = token.column;
    return expr;
}

static Stmt *new_stmt(StmtKind kind, Token token) {
    Stmt *stmt = xmalloc(sizeof(Stmt));
    memset(stmt, 0, sizeof(*stmt));
    stmt->kind = kind; stmt->line = token.line; stmt->column = token.column;
    return stmt;
}

static void append_expr(Expr ***items, size_t *count, Expr *value) {
    *items = xrealloc(*items, (*count + 1) * sizeof(Expr *));
    (*items)[(*count)++] = value;
}

static void append_stmt(Stmt ***items, size_t *count, Stmt *value) {
    *items = xrealloc(*items, (*count + 1) * sizeof(Stmt *));
    (*items)[(*count)++] = value;
}

static void append_function(Function ***items, size_t *count, Function *value) {
    *items = xrealloc(*items, (*count + 1) * sizeof(Function *));
    (*items)[(*count)++] = value;
}

typedef struct {
    Token *tokens;
    size_t count;
    size_t current;
    Source source;
    char error[512];
    int error_line;
    int error_column;
} Parser;

static Token *peek_token(Parser *parser) { return &parser->tokens[parser->current]; }
static Token *previous_token(Parser *parser) { return &parser->tokens[parser->current - 1]; }
static bool check_token(Parser *parser, TokenKind kind) { return peek_token(parser)->kind == kind; }
static Token *advance_token(Parser *parser) { if (parser->current < parser->count - 1) parser->current++; return previous_token(parser); }
static bool match_token(Parser *parser, TokenKind kind) { if (!check_token(parser, kind)) return false; advance_token(parser); return true; }

static char *token_text(Parser *parser, Token token) {
    return xstrndup0(parser->source.source + token.start, token.length);
}

static void parser_fail(Parser *parser, Token *token, const char *format, ...) {
    if (parser->error[0] != '\0') return;
    va_list args;
    va_start(args, format);
    vsnprintf(parser->error, sizeof(parser->error), format, args);
    va_end(args);
    parser->error_line = token->line;
    parser->error_column = token->column;
}

static Token *expect_token(Parser *parser, TokenKind kind, const char *message) {
    if (check_token(parser, kind)) return advance_token(parser);
    parser_fail(parser, peek_token(parser), "%s", message);
    return peek_token(parser);
}

static void skip_type(Parser *parser) {
    while (!check_token(parser, TOK_EQUAL) && !check_token(parser, TOK_COMMA) && !check_token(parser, TOK_RPAREN) && !check_token(parser, TOK_LBRACE) && !check_token(parser, TOK_EOF)) advance_token(parser);
}

static Expr *parse_expression(Parser *parser);
static Stmt *parse_statement(Parser *parser);

static Expr *parse_primary(Parser *parser) {
    Token token = *peek_token(parser);
    if (match_token(parser, TOK_INT)) {
        Expr *expr = new_expr(EXPR_INT, token); char *text = token_text(parser, token);
        expr->as.int_value = strtoll(text, NULL, 10); free(text); return expr;
    }
    if (match_token(parser, TOK_FLOAT)) {
        Expr *expr = new_expr(EXPR_FLOAT, token); char *text = token_text(parser, token);
        expr->as.float_value = strtod(text, NULL); free(text); return expr;
    }
    if (match_token(parser, TOK_STRING)) {
        Expr *expr = new_expr(EXPR_STRING, token); char *raw = token_text(parser, token);
        size_t cap = token.length + 1, len = 0; char *decoded = xmalloc(cap);
        for (size_t i = 0; i < token.length; i++) {
            char c = raw[i];
            if (c == '\\' && i + 1 < token.length) {
                char next = raw[++i];
                if (next == 'n') c = '\n'; else if (next == 'r') c = '\r'; else if (next == 't') c = '\t'; else if (next == '"') c = '"'; else if (next == '\\') c = '\\'; else c = next;
            }
            decoded[len++] = c;
        }
        decoded[len] = '\0'; expr->as.string_value = decoded; free(raw); return expr;
    }
    if (match_token(parser, TOK_TRUE) || match_token(parser, TOK_FALSE)) {
        bool value = previous_token(parser)->kind == TOK_TRUE;
        Expr *expr = new_expr(EXPR_BOOL, token); expr->as.bool_value = value; return expr;
    }
    if (match_token(parser, TOK_NIL)) { return new_expr(EXPR_NIL, token); }
    if (match_token(parser, TOK_ID)) {
        Expr *expr = new_expr(EXPR_VARIABLE, token); expr->as.variable = token_text(parser, token); return expr;
    }
    if (match_token(parser, TOK_LPAREN)) {
        Expr *expr = parse_expression(parser);
        expect_token(parser, TOK_RPAREN, "expected ')' after expression");
        return expr;
    }
    if (match_token(parser, TOK_LBRACKET)) {
        Expr *expr = new_expr(EXPR_ARRAY, token);
        while (!check_token(parser, TOK_RBRACKET) && !check_token(parser, TOK_EOF)) {
            append_expr(&expr->as.array.items, &expr->as.array.count, parse_expression(parser));
            if (!match_token(parser, TOK_COMMA)) break;
        }
        expect_token(parser, TOK_RBRACKET, "expected ']' after array literal");
        return expr;
    }
    parser_fail(parser, &token, "expected an expression");
    return new_expr(EXPR_NIL, token);
}

static Expr *parse_unary(Parser *parser) {
    if (match_token(parser, TOK_BANG) || match_token(parser, TOK_MINUS) || match_token(parser, TOK_PLUS)) {
        Token token = *previous_token(parser); Expr *expr = new_expr(EXPR_UNARY, token);
        expr->as.unary.op = token.kind; expr->as.unary.operand = parse_unary(parser); return expr;
    }
    Expr *expr = parse_primary(parser);
    while (true) {
        if (match_token(parser, TOK_LPAREN)) {
            if (expr->kind != EXPR_VARIABLE) parser_fail(parser, previous_token(parser), "only named functions can be called");
            Expr *call = new_expr(EXPR_CALL, *previous_token(parser));
            call->as.call.name = expr->kind == EXPR_VARIABLE ? xstrdup0(expr->as.variable) : xstrdup0("<invalid>");
            while (!check_token(parser, TOK_RPAREN) && !check_token(parser, TOK_EOF)) {
                append_expr(&call->as.call.args, &call->as.call.count, parse_expression(parser));
                if (!match_token(parser, TOK_COMMA)) break;
            }
            expect_token(parser, TOK_RPAREN, "expected ')' after arguments"); expr = call;
        } else if (match_token(parser, TOK_LBRACKET)) {
            Expr *index = new_expr(EXPR_INDEX, *previous_token(parser)); index->as.index.base = expr;
            index->as.index.index = parse_expression(parser); expect_token(parser, TOK_RBRACKET, "expected ']' after index"); expr = index;
        } else break;
    }
    return expr;
}

static int precedence(TokenKind kind) {
    switch (kind) {
        case TOK_OR_OR: return 1; case TOK_AND_AND: return 2;
        case TOK_EQUAL_EQUAL: case TOK_BANG_EQUAL: return 3;
        case TOK_LESS: case TOK_LESS_EQUAL: case TOK_GREATER: case TOK_GREATER_EQUAL: return 4;
        case TOK_PLUS: case TOK_MINUS: return 5;
        case TOK_STAR: case TOK_SLASH: case TOK_PERCENT: return 6;
        default: return 0;
    }
}

static Expr *parse_precedence(Parser *parser, int minimum) {
    Expr *left = parse_unary(parser);
    while (precedence(peek_token(parser)->kind) >= minimum && precedence(peek_token(parser)->kind) > 0) {
        Token op = *advance_token(parser); int next = precedence(op.kind) + 1;
        Expr *right = parse_precedence(parser, next); Expr *binary = new_expr(EXPR_BINARY, op);
        binary->as.binary.op = op.kind; binary->as.binary.left = left; binary->as.binary.right = right; left = binary;
    }
    return left;
}

static Expr *parse_expression(Parser *parser) { return parse_precedence(parser, 1); }

static void consume_statement_end(Parser *parser) { while (match_token(parser, TOK_SEMICOLON)) {} }

static void parse_block(Parser *parser, Stmt ***items, size_t *count) {
    expect_token(parser, TOK_LBRACE, "expected '{'");
    while (!check_token(parser, TOK_RBRACE) && !check_token(parser, TOK_EOF) && parser->error[0] == '\0') {
        Stmt *statement = parse_statement(parser); if (statement) append_stmt(items, count, statement); consume_statement_end(parser);
    }
    expect_token(parser, TOK_RBRACE, "expected '}' after block");
}

static Stmt *parse_if_statement(Parser *parser) {
    Token token = *previous_token(parser); Stmt *stmt = new_stmt(STMT_IF, token);
    stmt->as.if_stmt.condition = parse_expression(parser);
    parse_block(parser, &stmt->as.if_stmt.then_body, &stmt->as.if_stmt.then_count);
    if (match_token(parser, TOK_ELSE)) {
        if (match_token(parser, TOK_IF)) {
            Stmt *nested = parse_if_statement(parser); append_stmt(&stmt->as.if_stmt.else_body, &stmt->as.if_stmt.else_count, nested);
        } else parse_block(parser, &stmt->as.if_stmt.else_body, &stmt->as.if_stmt.else_count);
    }
    return stmt;
}

static Stmt *parse_statement(Parser *parser) {
    Token token = *peek_token(parser);
    if (match_token(parser, TOK_LET)) {
        match_token(parser, TOK_MUT); Token *name = expect_token(parser, TOK_ID, "expected binding name after 'let'");
        if (match_token(parser, TOK_COLON)) skip_type(parser);
        expect_token(parser, TOK_EQUAL, "expected '=' in binding"); Stmt *stmt = new_stmt(STMT_LET, token);
        stmt->as.let.name = token_text(parser, *name); stmt->as.let.initializer = parse_expression(parser); return stmt;
    }
    if (match_token(parser, TOK_IF)) return parse_if_statement(parser);
    if (match_token(parser, TOK_WHILE)) {
        Stmt *stmt = new_stmt(STMT_WHILE, token); stmt->as.while_stmt.condition = parse_expression(parser);
        parse_block(parser, &stmt->as.while_stmt.body, &stmt->as.while_stmt.count); return stmt;
    }
    if (match_token(parser, TOK_RETURN)) {
        Stmt *stmt = new_stmt(STMT_RETURN, token);
        if (check_token(parser, TOK_RBRACE) || check_token(parser, TOK_EOF) || check_token(parser, TOK_SEMICOLON)) stmt->as.return_value = NULL;
        else stmt->as.return_value = parse_expression(parser);
        return stmt;
    }
    if (match_token(parser, TOK_BREAK)) return new_stmt(STMT_BREAK, token);
    if (match_token(parser, TOK_CONTINUE)) return new_stmt(STMT_CONTINUE, token);
    if (match_token(parser, TOK_PUB)) {
        parser_fail(parser, &token, "only 'pub fn' declarations are supported by the native core"); return NULL;
    }
    if (check_token(parser, TOK_IMPORT) || check_token(parser, TOK_STRUCT) || check_token(parser, TOK_ENUM) || check_token(parser, TOK_MATCH)) {
        advance_token(parser); parser_fail(parser, &token, "this declaration is not available in the native core yet"); return NULL;
    }
    if (check_token(parser, TOK_ID) && parser->current + 1 < parser->count && parser->tokens[parser->current + 1].kind == TOK_EQUAL) {
        Token name = *advance_token(parser); advance_token(parser); Stmt *stmt = new_stmt(STMT_ASSIGN, name);
        stmt->as.assign.name = token_text(parser, name); stmt->as.assign.value = parse_expression(parser); return stmt;
    }
    Stmt *stmt = new_stmt(STMT_EXPR, token); stmt->as.expression = parse_expression(parser); return stmt;
}

static Function *parse_function(Parser *parser) {
    Token fn_token = *expect_token(parser, TOK_FN, "expected 'fn'");
    Token *name = expect_token(parser, TOK_ID, "expected function name");
    Function *function = xmalloc(sizeof(Function)); memset(function, 0, sizeof(*function)); function->name = token_text(parser, *name);
    expect_token(parser, TOK_LPAREN, "expected '(' after function name");
    while (!check_token(parser, TOK_RPAREN) && !check_token(parser, TOK_EOF)) {
        Token *param = expect_token(parser, TOK_ID, "expected parameter name");
        function->parameters = xrealloc(function->parameters, (function->parameter_count + 1) * sizeof(Parameter));
        function->parameters[function->parameter_count++].name = token_text(parser, *param);
        if (match_token(parser, TOK_COLON)) skip_type(parser);
        if (!match_token(parser, TOK_COMMA)) break;
    }
    expect_token(parser, TOK_RPAREN, "expected ')' after parameters");
    if (match_token(parser, TOK_ARROW)) skip_type(parser);
    if (parser->error[0] == '\0') parse_block(parser, &function->body, &function->body_count);
    (void)fn_token;
    return function;
}

static Program *parse_program(Lexer *lexer) {
    Parser parser = {lexer->items, lexer->count, 0, lexer->source, "", 0, 0};
    Program *program = xmalloc(sizeof(Program)); memset(program, 0, sizeof(*program));
    while (!check_token(&parser, TOK_EOF) && parser.error[0] == '\0') {
        if (check_token(&parser, TOK_FN)) append_function(&program->functions, &program->function_count, parse_function(&parser));
        else if (check_token(&parser, TOK_PUB)) {
            advance_token(&parser);
            if (check_token(&parser, TOK_FN)) append_function(&program->functions, &program->function_count, parse_function(&parser));
            else parser_fail(&parser, previous_token(&parser), "'pub' must be followed by a function in the native core");
        } else {
            Stmt *statement = parse_statement(&parser); if (statement) append_stmt(&program->statements, &program->statement_count, statement); consume_statement_end(&parser);
        }
    }
    if (parser.error[0] != '\0') {
        fprintf(stderr, "kry: %s:%d:%d: error: %s\n", lexer->source.name, parser.error_line, parser.error_column, parser.error);
        return NULL;
    }
    return program;
}

typedef enum { VAL_NIL, VAL_INT, VAL_FLOAT, VAL_BOOL, VAL_STRING, VAL_ARRAY, VAL_BYTES } ValueType;

typedef struct Value Value;
typedef struct {
    char *data;
    size_t length;
} StringValue;
typedef struct {
    Value *items;
    size_t length;
} ArrayValue;
typedef struct {
    unsigned char *data;
    size_t length;
} BytesValue;
struct Value {
    ValueType type;
    union { int64_t integer; double floating; int boolean; StringValue string; ArrayValue array; BytesValue bytes; } as;
};

typedef struct Environment Environment;
struct Environment {
    char **names;
    Value *values;
    size_t count;
    size_t capacity;
    Environment *parent;
};

typedef struct {
    Program *program;
    const char *filename;
    char error[512];
    int error_line;
    int error_column;
} Runtime;

typedef enum { EXEC_NORMAL, EXEC_RETURN, EXEC_BREAK, EXEC_CONTINUE, EXEC_ERROR } ExecCode;
typedef struct { ExecCode code; Value value; } ExecResult;

static Value value_nil(void) { return (Value){VAL_NIL, {0}}; }
static Value value_int(int64_t value) { Value result = {VAL_INT, {0}}; result.as.integer = value; return result; }
static Value value_float(double value) { Value result = {VAL_FLOAT, {0}}; result.as.floating = value; return result; }
static Value value_bool(bool value) { Value result = {VAL_BOOL, {0}}; result.as.boolean = value ? 1 : 0; return result; }
static Value value_string_n(const char *text, size_t length) { Value result = {VAL_STRING, {0}}; result.as.string.data = xstrndup0(text, length); result.as.string.length = length; return result; }
static Value value_string(const char *text) { return value_string_n(text, strlen(text)); }
static Value value_bytes_n(const unsigned char *data, size_t length) {
    Value result = {VAL_BYTES, {0}};
    result.as.bytes.data = xmalloc(length == 0 ? 1 : length);
    if (length > 0) memcpy(result.as.bytes.data, data, length);
    result.as.bytes.length = length;
    return result;
}

static Environment *new_environment(Environment *parent) {
    Environment *env = xmalloc(sizeof(Environment)); memset(env, 0, sizeof(*env)); env->parent = parent; return env;
}

static bool env_define(Environment *env, const char *name, Value value) {
    for (size_t i = 0; i < env->count; i++) if (strcmp(env->names[i], name) == 0) return false;
    if (env->count == env->capacity) { env->capacity = env->capacity == 0 ? 8 : env->capacity * 2; env->names = xrealloc(env->names, env->capacity * sizeof(char *)); env->values = xrealloc(env->values, env->capacity * sizeof(Value)); }
    env->names[env->count] = xstrdup0(name); env->values[env->count++] = value; return true;
}

static bool env_get(Environment *env, const char *name, Value *out) {
    for (Environment *current = env; current; current = current->parent) for (size_t i = 0; i < current->count; i++) if (strcmp(current->names[i], name) == 0) { *out = current->values[i]; return true; }
    return false;
}

static bool env_set(Environment *env, const char *name, Value value) {
    for (Environment *current = env; current; current = current->parent) for (size_t i = 0; i < current->count; i++) if (strcmp(current->names[i], name) == 0) { current->values[i] = value; return true; }
    return false;
}

static bool value_truthy(Value value) {
    if (value.type == VAL_NIL) return false;
    if (value.type == VAL_BOOL) return value.as.boolean != 0;
    if (value.type == VAL_INT) return value.as.integer != 0;
    if (value.type == VAL_FLOAT) return value.as.floating != 0.0;
    return true;
}

static bool value_equal(Value left, Value right) {
    if (left.type != right.type) {
        if ((left.type == VAL_INT || left.type == VAL_FLOAT) && (right.type == VAL_INT || right.type == VAL_FLOAT)) {
            double a = left.type == VAL_INT ? (double)left.as.integer : left.as.floating;
            double b = right.type == VAL_INT ? (double)right.as.integer : right.as.floating;
            return a == b;
        }
        return false;
    }
    switch (left.type) {
        case VAL_NIL: return true; case VAL_INT: return left.as.integer == right.as.integer;
        case VAL_FLOAT: return left.as.floating == right.as.floating; case VAL_BOOL: return left.as.boolean == right.as.boolean;
        case VAL_STRING: return left.as.string.length == right.as.string.length && memcmp(left.as.string.data, right.as.string.data, left.as.string.length) == 0;
        case VAL_BYTES: return left.as.bytes.length == right.as.bytes.length && memcmp(left.as.bytes.data, right.as.bytes.data, left.as.bytes.length) == 0;
        case VAL_ARRAY:
            if (left.as.array.length != right.as.array.length) return false;
            for (size_t i = 0; i < left.as.array.length; i++) if (!value_equal(left.as.array.items[i], right.as.array.items[i])) return false;
            return true;
    }
    return false;
}

static void print_value(FILE *out, Value value) {
    switch (value.type) {
        case VAL_NIL: fputs("nil", out); break;
        case VAL_INT: fprintf(out, "%" PRId64, value.as.integer); break;
        case VAL_FLOAT: fprintf(out, "%g", value.as.floating); break;
        case VAL_BOOL: fputs(value.as.boolean ? "true" : "false", out); break;
        case VAL_STRING: fwrite(value.as.string.data, 1, value.as.string.length, out); break;
        case VAL_ARRAY:
            fputc('[', out); for (size_t i = 0; i < value.as.array.length; i++) { if (i) fputs(", ", out); print_value(out, value.as.array.items[i]); } fputc(']', out); break;
        case VAL_BYTES: fprintf(out, "<Bytes:%zu>", value.as.bytes.length); break;
    }
}

static void runtime_fail(Runtime *runtime, int line, int column, const char *format, ...) {
    if (runtime->error[0] != '\0') return;
    va_list args; va_start(args, format); vsnprintf(runtime->error, sizeof(runtime->error), format, args); va_end(args);
    runtime->error_line = line; runtime->error_column = column;
}

static ExecResult normal_result(void) { return (ExecResult){EXEC_NORMAL, value_nil()}; }
static ExecResult return_result(Value value) { return (ExecResult){EXEC_RETURN, value}; }
static ExecResult control_result(ExecCode code) { return (ExecResult){code, value_nil()}; }

static Value eval_expr(Runtime *runtime, Environment *env, Expr *expr);
static ExecResult execute_statement(Runtime *runtime, Environment *env, Stmt *stmt);

static Function *find_function(Program *program, const char *name) {
    for (size_t i = 0; i < program->function_count; i++) if (strcmp(program->functions[i]->name, name) == 0) return program->functions[i];
    return NULL;
}

static size_t utf8_width(unsigned char c) {
    if (c < 0x80) return 1;
    if (c >= 0xC2 && c <= 0xDF) return 2;
    if (c >= 0xE0 && c <= 0xEF) return 3;
    if (c >= 0xF0 && c <= 0xF4) return 4;
    return 0;
}

static bool valid_utf8(const unsigned char *data, size_t length) {
    size_t i = 0;
    while (i < length) {
        unsigned char c = data[i];
        size_t width = utf8_width(c);
        if (width == 0 || i + width > length) return false;
        if (width >= 3 && ((c == 0xE0 && data[i + 1] < 0xA0) || (c == 0xED && data[i + 1] >= 0xA0) || (c == 0xF0 && data[i + 1] < 0x90) || (c == 0xF4 && data[i + 1] >= 0x90))) return false;
        for (size_t j = 1; j < width; j++) if ((data[i + j] & 0xC0) != 0x80) return false;
        i += width;
    }
    return true;
}

static size_t utf8_codepoint_count(const char *data, size_t length) {
    size_t count = 0;
    for (size_t i = 0; i < length; i += utf8_width((unsigned char)data[i])) count++;
    return count;
}

static Value builtin_call(Runtime *runtime, Expr *expr, Value *args, size_t count) {
    const char *name = expr->as.call.name;
    if (strcmp(name, "print") == 0 || strcmp(name, "println") == 0) {
        if (count != 1) { runtime_fail(runtime, expr->line, expr->column, "%s expects one argument", name); return value_nil(); }
        print_value(stdout, args[0]); if (strcmp(name, "println") == 0) fputc('\n', stdout); fflush(stdout); return value_nil();
    }
    if (strcmp(name, "len") == 0) {
        if (count != 1 || (args[0].type != VAL_STRING && args[0].type != VAL_ARRAY && args[0].type != VAL_BYTES)) { runtime_fail(runtime, expr->line, expr->column, "len expects a String, Array, or Bytes"); return value_nil(); }
        if (args[0].type == VAL_STRING) {
            if (!valid_utf8((const unsigned char *)args[0].as.string.data, args[0].as.string.length)) { runtime_fail(runtime, expr->line, expr->column, "len received invalid UTF-8 String"); return value_nil(); }
            return value_int((int64_t)utf8_codepoint_count(args[0].as.string.data, args[0].as.string.length));
        }
        if (args[0].type == VAL_ARRAY) return value_int((int64_t)args[0].as.array.length);
        return value_int((int64_t)args[0].as.bytes.length);
    }
    if (strcmp(name, "bytes") == 0) {
        if (count != 1 || args[0].type != VAL_ARRAY) { runtime_fail(runtime, expr->line, expr->column, "bytes expects an Array of octets"); return value_nil(); }
        unsigned char *data = xmalloc(args[0].as.array.length == 0 ? 1 : args[0].as.array.length);
        for (size_t i = 0; i < args[0].as.array.length; i++) {
            if (args[0].as.array.items[i].type != VAL_INT || args[0].as.array.items[i].as.integer < 0 || args[0].as.array.items[i].as.integer > 255) { free(data); runtime_fail(runtime, expr->line, expr->column, "bytes accepts only integers in 0..255"); return value_nil(); }
            data[i] = (unsigned char)args[0].as.array.items[i].as.integer;
        }
        Value result = value_bytes_n(data, args[0].as.array.length); free(data); return result;
    }
    if (strcmp(name, "string_to_bytes") == 0) {
        if (count != 1 || args[0].type != VAL_STRING || !valid_utf8((const unsigned char *)args[0].as.string.data, args[0].as.string.length)) { runtime_fail(runtime, expr->line, expr->column, "string_to_bytes expects valid UTF-8 String"); return value_nil(); }
        return value_bytes_n((const unsigned char *)args[0].as.string.data, args[0].as.string.length);
    }
    if (strcmp(name, "bytes_to_string") == 0) {
        if (count != 1 || args[0].type != VAL_BYTES || !valid_utf8(args[0].as.bytes.data, args[0].as.bytes.length)) { runtime_fail(runtime, expr->line, expr->column, "bytes_to_string received invalid UTF-8"); return value_nil(); }
        return value_string_n((const char *)args[0].as.bytes.data, args[0].as.bytes.length);
    }
    if (strcmp(name, "array_push") == 0) {
        if (count != 2 || args[0].type != VAL_ARRAY) { runtime_fail(runtime, expr->line, expr->column, "array_push expects an Array and a value"); return value_nil(); }
        Value result = {VAL_ARRAY, {0}}; result.as.array.length = args[0].as.array.length + 1; result.as.array.items = xmalloc(result.as.array.length * sizeof(Value));
        for (size_t i = 0; i < args[0].as.array.length; i++) {
            result.as.array.items[i] = args[0].as.array.items[i];
        }
        result.as.array.items[result.as.array.length - 1] = args[1];
        return result;
    }
    if (strcmp(name, "abs") == 0) {
        if (count != 1 || (args[0].type != VAL_INT && args[0].type != VAL_FLOAT)) { runtime_fail(runtime, expr->line, expr->column, "abs expects a number"); return value_nil(); }
        if (args[0].type == VAL_INT) return value_int(args[0].as.integer < 0 ? -args[0].as.integer : args[0].as.integer);
        return value_float(args[0].as.floating < 0 ? -args[0].as.floating : args[0].as.floating);
    }
    if (strcmp(name, "assert") == 0) {
        if (count != 1 || !value_truthy(args[0])) { runtime_fail(runtime, expr->line, expr->column, "assertion failed"); return value_nil(); } return value_nil();
    }
    if (strcmp(name, "assert_eq") == 0) {
        if (count != 2 || !value_equal(args[0], args[1])) { runtime_fail(runtime, expr->line, expr->column, "assert_eq failed"); return value_nil(); } return value_nil();
    }
    if (strcmp(name, "sqrt") == 0) {
        if (count != 1 || (args[0].type != VAL_INT && args[0].type != VAL_FLOAT)) { runtime_fail(runtime, expr->line, expr->column, "sqrt expects a number"); return value_nil(); }
        double input = args[0].type == VAL_INT ? (double)args[0].as.integer : args[0].as.floating;
        if (input < 0) { runtime_fail(runtime, expr->line, expr->column, "sqrt expects a non-negative number"); return value_nil(); }
        if (input == 0) return value_float(0);
        double guess = input > 1 ? input : 1;
        for (int i = 0; i < 32; i++) guess = (guess + input / guess) / 2.0;
        return value_float(guess);
    }
    return value_nil();
}

static Value call_function(Runtime *runtime, Environment *caller, Expr *expr, Value *args, size_t count) {
    if (strcmp(expr->as.call.name, "print") == 0 || strcmp(expr->as.call.name, "println") == 0 || strcmp(expr->as.call.name, "len") == 0 || strcmp(expr->as.call.name, "bytes") == 0 || strcmp(expr->as.call.name, "string_to_bytes") == 0 || strcmp(expr->as.call.name, "bytes_to_string") == 0 || strcmp(expr->as.call.name, "array_push") == 0 || strcmp(expr->as.call.name, "abs") == 0 || strcmp(expr->as.call.name, "assert") == 0 || strcmp(expr->as.call.name, "assert_eq") == 0 || strcmp(expr->as.call.name, "sqrt") == 0) return builtin_call(runtime, expr, args, count);
    Function *function = find_function(runtime->program, expr->as.call.name);
    if (!function) { runtime_fail(runtime, expr->line, expr->column, "unknown function '%s'", expr->as.call.name); return value_nil(); }
    if (count != function->parameter_count) { runtime_fail(runtime, expr->line, expr->column, "function '%s' expects %zu argument(s), got %zu", function->name, function->parameter_count, count); return value_nil(); }
    Environment *local = new_environment(caller);
    for (size_t i = 0; i < count; i++) env_define(local, function->parameters[i].name, args[i]);
    for (size_t i = 0; i < function->body_count; i++) {
        ExecResult result = execute_statement(runtime, local, function->body[i]);
        if (result.code == EXEC_RETURN) return result.value;
        if (result.code != EXEC_NORMAL) { if (result.code == EXEC_BREAK || result.code == EXEC_CONTINUE) runtime_fail(runtime, function->body[i]->line, function->body[i]->column, "loop control used outside a loop"); return value_nil(); }
        if (runtime->error[0] != '\0') return value_nil();
    }
    return value_nil();
}

static bool numeric_values(Value value) { return value.type == VAL_INT || value.type == VAL_FLOAT; }
static double numeric_as_double(Value value) { return value.type == VAL_INT ? (double)value.as.integer : value.as.floating; }

static Value eval_expr(Runtime *runtime, Environment *env, Expr *expr) {
    if (runtime->error[0] != '\0') return value_nil();
    switch (expr->kind) {
        case EXPR_INT: return value_int(expr->as.int_value); case EXPR_FLOAT: return value_float(expr->as.float_value);
        case EXPR_BOOL: return value_bool(expr->as.bool_value); case EXPR_NIL: return value_nil();
        case EXPR_STRING: return value_string(expr->as.string_value);
        case EXPR_VARIABLE: { Value value; if (!env_get(env, expr->as.variable, &value)) runtime_fail(runtime, expr->line, expr->column, "unknown variable '%s'", expr->as.variable); return value; }
        case EXPR_ARRAY: {
            Value value = {VAL_ARRAY, {0}}; value.as.array.length = expr->as.array.count; value.as.array.items = xmalloc(value.as.array.length * sizeof(Value));
            for (size_t i = 0; i < expr->as.array.count; i++) {
                value.as.array.items[i] = eval_expr(runtime, env, expr->as.array.items[i]);
            }
            return value;
        }
        case EXPR_UNARY: {
            Value operand = eval_expr(runtime, env, expr->as.unary.operand);
            if (expr->as.unary.op == TOK_BANG) return value_bool(!value_truthy(operand));
            if (!numeric_values(operand)) { runtime_fail(runtime, expr->line, expr->column, "unary operator expects a number"); return value_nil(); }
            if (expr->as.unary.op == TOK_MINUS) return operand.type == VAL_INT ? value_int(-operand.as.integer) : value_float(-operand.as.floating);
            return operand;
        }
        case EXPR_BINARY: {
            int op = expr->as.binary.op;
            Value left = eval_expr(runtime, env, expr->as.binary.left);
            if (op == TOK_AND_AND && !value_truthy(left)) return value_bool(false);
            if (op == TOK_OR_OR && value_truthy(left)) return value_bool(true);
            Value right = eval_expr(runtime, env, expr->as.binary.right);
            if (op == TOK_AND_AND || op == TOK_OR_OR) return value_bool(value_truthy(left) && value_truthy(right));
            if (op == TOK_EQUAL_EQUAL) return value_bool(value_equal(left, right));
            if (op == TOK_BANG_EQUAL) return value_bool(!value_equal(left, right));
            if (op == TOK_PLUS && left.type == VAL_STRING && right.type == VAL_STRING) {
                Value value = {VAL_STRING, {0}}; value.as.string.length = left.as.string.length + right.as.string.length; value.as.string.data = xmalloc(value.as.string.length + 1);
                memcpy(value.as.string.data, left.as.string.data, left.as.string.length); memcpy(value.as.string.data + left.as.string.length, right.as.string.data, right.as.string.length); value.as.string.data[value.as.string.length] = '\0'; return value;
            }
            if (op == TOK_PLUS && left.type == VAL_ARRAY && right.type == VAL_ARRAY) {
                Value value = {VAL_ARRAY, {0}}; value.as.array.length = left.as.array.length + right.as.array.length; value.as.array.items = xmalloc(value.as.array.length * sizeof(Value));
                memcpy(value.as.array.items, left.as.array.items, left.as.array.length * sizeof(Value)); memcpy(value.as.array.items + left.as.array.length, right.as.array.items, right.as.array.length * sizeof(Value)); return value;
            }
            if (op == TOK_PLUS && left.type == VAL_BYTES && right.type == VAL_BYTES) {
                Value value = value_bytes_n(NULL, left.as.bytes.length + right.as.bytes.length);
                memcpy(value.as.bytes.data, left.as.bytes.data, left.as.bytes.length); memcpy(value.as.bytes.data + left.as.bytes.length, right.as.bytes.data, right.as.bytes.length); return value;
            }
            if (!numeric_values(left) || !numeric_values(right)) { runtime_fail(runtime, expr->line, expr->column, "operator requires compatible numeric values"); return value_nil(); }
            bool floating = left.type == VAL_FLOAT || right.type == VAL_FLOAT; double a = numeric_as_double(left), b = numeric_as_double(right);
            if (op == TOK_LESS) return value_bool(a < b);
            if (op == TOK_LESS_EQUAL) return value_bool(a <= b);
            if (op == TOK_GREATER) return value_bool(a > b);
            if (op == TOK_GREATER_EQUAL) return value_bool(a >= b);
            if (op == TOK_PLUS) return floating ? value_float(a + b) : value_int(left.as.integer + right.as.integer);
            if (op == TOK_MINUS) return floating ? value_float(a - b) : value_int(left.as.integer - right.as.integer);
            if (op == TOK_STAR) return floating ? value_float(a * b) : value_int(left.as.integer * right.as.integer);
            if (op == TOK_SLASH) { if (b == 0) { runtime_fail(runtime, expr->line, expr->column, "division by zero"); return value_nil(); } return floating ? value_float(a / b) : value_int(left.as.integer / right.as.integer); }
            if (op == TOK_PERCENT) { if (right.type != VAL_INT || right.as.integer == 0 || left.type != VAL_INT) { runtime_fail(runtime, expr->line, expr->column, "remainder requires non-zero integers"); return value_nil(); } return value_int(left.as.integer % right.as.integer); }
            runtime_fail(runtime, expr->line, expr->column, "unsupported binary operator"); return value_nil();
        }
        case EXPR_CALL: {
            Value *args = xmalloc(expr->as.call.count * sizeof(Value));
            for (size_t i = 0; i < expr->as.call.count; i++) {
                args[i] = eval_expr(runtime, env, expr->as.call.args[i]);
                if (runtime->error[0] != '\0') { free(args); return value_nil(); }
            }
            Value result = call_function(runtime, env, expr, args, expr->as.call.count);
            free(args);
            return result;
        }
        case EXPR_INDEX: {
            Value base = eval_expr(runtime, env, expr->as.index.base); Value index = eval_expr(runtime, env, expr->as.index.index);
            if (index.type != VAL_INT || index.as.integer < 0) { runtime_fail(runtime, expr->line, expr->column, "index must be a non-negative integer"); return value_nil(); }
            size_t position = (size_t)index.as.integer;
            if (base.type == VAL_ARRAY) { if (position >= base.as.array.length) { runtime_fail(runtime, expr->line, expr->column, "array index out of bounds"); return value_nil(); } return base.as.array.items[position]; }
            if (base.type == VAL_STRING) {
                if (!valid_utf8((const unsigned char *)base.as.string.data, base.as.string.length)) { runtime_fail(runtime, expr->line, expr->column, "indexing received invalid UTF-8 String"); return value_nil(); }
                size_t offset = 0;
                for (size_t current = 0; current < position && offset < base.as.string.length; current++) offset += utf8_width((unsigned char)base.as.string.data[offset]);
                if (offset >= base.as.string.length) { runtime_fail(runtime, expr->line, expr->column, "string index out of bounds"); return value_nil(); }
                return value_string_n(base.as.string.data + offset, utf8_width((unsigned char)base.as.string.data[offset]));
            }
            if (base.type == VAL_BYTES) { if (position >= base.as.bytes.length) { runtime_fail(runtime, expr->line, expr->column, "bytes index out of bounds"); return value_nil(); } return value_int(base.as.bytes.data[position]); }
            runtime_fail(runtime, expr->line, expr->column, "indexing requires a String, Array, or Bytes"); return value_nil();
        }
    }
    return value_nil();
}

static ExecResult execute_statement(Runtime *runtime, Environment *env, Stmt *stmt) {
    if (runtime->error[0] != '\0') return control_result(EXEC_ERROR);
    switch (stmt->kind) {
        case STMT_LET: { Value value = eval_expr(runtime, env, stmt->as.let.initializer); if (!env_define(env, stmt->as.let.name, value)) runtime_fail(runtime, stmt->line, stmt->column, "binding '%s' already exists in this scope", stmt->as.let.name); return normal_result(); }
        case STMT_EXPR: (void)eval_expr(runtime, env, stmt->as.expression); return normal_result();
        case STMT_ASSIGN: { Value value = eval_expr(runtime, env, stmt->as.assign.value); if (!env_set(env, stmt->as.assign.name, value)) runtime_fail(runtime, stmt->line, stmt->column, "unknown variable '%s'", stmt->as.assign.name); return normal_result(); }
        case STMT_RETURN: return return_result(stmt->as.return_value ? eval_expr(runtime, env, stmt->as.return_value) : value_nil());
        case STMT_BREAK: return control_result(EXEC_BREAK); case STMT_CONTINUE: return control_result(EXEC_CONTINUE);
        case STMT_IF: {
            Value condition = eval_expr(runtime, env, stmt->as.if_stmt.condition); Stmt **body = value_truthy(condition) ? stmt->as.if_stmt.then_body : stmt->as.if_stmt.else_body; size_t count = value_truthy(condition) ? stmt->as.if_stmt.then_count : stmt->as.if_stmt.else_count;
            Environment *branch = new_environment(env); for (size_t i = 0; i < count; i++) { ExecResult result = execute_statement(runtime, branch, body[i]); if (result.code != EXEC_NORMAL) return result; } return normal_result();
        }
        case STMT_WHILE: {
            while (value_truthy(eval_expr(runtime, env, stmt->as.while_stmt.condition))) {
                Environment *loop = new_environment(env);
                for (size_t i = 0; i < stmt->as.while_stmt.count; i++) { ExecResult result = execute_statement(runtime, loop, stmt->as.while_stmt.body[i]); if (result.code == EXEC_BREAK) return normal_result(); if (result.code == EXEC_CONTINUE) break; if (result.code != EXEC_NORMAL) return result; }
                if (runtime->error[0] != '\0') return control_result(EXEC_ERROR);
            }
            return normal_result();
        }
    }
    return normal_result();
}

static int run_program(Program *program, const char *filename) {
    Runtime runtime = {program, filename, "", 0, 0}; Environment *global = new_environment(NULL);
    for (size_t i = 0; i < program->statement_count; i++) {
        ExecResult result = execute_statement(&runtime, global, program->statements[i]);
        if (result.code == EXEC_RETURN) { runtime_fail(&runtime, program->statements[i]->line, program->statements[i]->column, "return is only valid inside a function"); break; }
        if (result.code == EXEC_BREAK || result.code == EXEC_CONTINUE) { runtime_fail(&runtime, program->statements[i]->line, program->statements[i]->column, "loop control is only valid inside a loop"); break; }
        if (runtime.error[0] != '\0') break;
    }
    if (runtime.error[0] != '\0') { fprintf(stderr, "kry: %s:%d:%d: runtime error: %s\n", filename, runtime.error_line, runtime.error_column, runtime.error); return 1; }
    return 0;
}

static char *read_file(const char *path, size_t *length) {
    FILE *file = fopen(path, "rb"); if (!file) { fprintf(stderr, "kry: cannot open '%s': %s\n", path, strerror(errno)); return NULL; }
    if (fseek(file, 0, SEEK_END) != 0) { fclose(file); return NULL; } long size = ftell(file); if (size < 0) { fclose(file); return NULL; }
    rewind(file); char *data = xmalloc((size_t)size + 1); size_t got = fread(data, 1, (size_t)size, file); fclose(file); if (got != (size_t)size) { free(data); fprintf(stderr, "kry: cannot read '%s'\n", path); return NULL; }
    data[got] = '\0'; *length = got; return data;
}

static bool has_magic(const char *data, size_t length) { const char magic[] = "KRYNATIVE1\n"; return length >= sizeof(magic) - 1 && memcmp(data, magic, sizeof(magic) - 1) == 0; }

static char *artifact_source(char *data, size_t length, size_t *source_length) {
    const size_t header = sizeof("KRYNATIVE1\n") - 1;
    if (length < header + 8) return NULL;
    uint64_t encoded = 0;
    for (size_t i = 0; i < 8; i++) encoded |= ((uint64_t)(unsigned char)data[header + i]) << (8U * i);
    if (encoded != length - header - 8) return NULL;
    *source_length = (size_t)encoded; char *source = xmalloc(*source_length + 1); memcpy(source, data + header + 8, *source_length); source[*source_length] = '\0'; return source;
}

static bool write_artifact(const char *path, const char *source, size_t length) {
    FILE *file = fopen(path, "wb"); if (!file) { fprintf(stderr, "kry: cannot create '%s': %s\n", path, strerror(errno)); return false; }
    const char magic[] = "KRYNATIVE1\n"; bool ok = fwrite(magic, 1, sizeof(magic) - 1, file) == sizeof(magic) - 1;
    uint64_t size = (uint64_t)length; for (int i = 0; i < 8 && ok; i++) { unsigned char byte = (unsigned char)((size >> (8 * i)) & 255); ok = fwrite(&byte, 1, 1, file) == 1; }
    if (ok) ok = fwrite(source, 1, length, file) == length;
    if (fclose(file) != 0) ok = false;
    if (!ok) fprintf(stderr, "kry: failed writing '%s'\n", path);
    return ok;
}

static int execute_input(const char *path, char *data, size_t length, bool check_only) {
    char *source_text = data;
    size_t source_length = length;
    bool owns_source = false;
    if (has_magic(data, length)) {
        source_text = artifact_source(data, length, &source_length);
        if (!source_text) { fprintf(stderr, "kry: %s: malformed native artifact\n", path); return 1; }
        owns_source = true;
    }
    Source source = {path, source_text, source_length, 1, 1, 0};
    Lexer lexer = {NULL, 0, 0, source, "", 0, 0, 1, 1};
    lex_source(&lexer);
    if (lexer.error[0] != '\0') {
        fprintf(stderr, "kry: %s:%d:%d: error: %s\n", path, lexer.error_line, lexer.error_column, lexer.error);
        if (owns_source) free(source_text);
        return 1;
    }
    Program *program = parse_program(&lexer);
    if (!program) { if (owns_source) free(source_text); return 1; }
    if (check_only) { if (owns_source) free(source_text); return 0; }
    int result = run_program(program, path);
    if (owns_source) free(source_text);
    return result;
}

static void usage(void) {
    puts("Kryndel native 0.2.0");
    puts("Usage:");
    puts("  kry check <file.kry>       Parse and validate a source file");
    puts("  kry run <file.kry|.kexe>  Execute a source file or native artifact");
    puts("  kry build <file.kry> [-o <file.kexe>]  Package source as a native artifact");
    puts("  kry <file.kry>             Shorthand for 'kry run <file.kry>'");
}

static char *default_artifact_path(const char *input) {
    const char *dot = strrchr(input, '.'); size_t length = dot ? (size_t)(dot - input) : strlen(input); char *output = xmalloc(length + 6); memcpy(output, input, length); memcpy(output + length, ".kexe", 6); return output;
}

int main(int argc, char **argv) {
    if (argc < 2 || strcmp(argv[1], "--help") == 0 || strcmp(argv[1], "-h") == 0) { usage(); return argc < 2 ? 2 : 0; }
    if (strcmp(argv[1], "--version") == 0) { puts("Kryndel native 0.2.0"); return 0; }
    const char *command = argv[1]; const char *input = NULL; bool check_only = false; bool build = false; const char *output = NULL;
    if (strcmp(command, "check") == 0) { if (argc != 3) { usage(); return 2; } input = argv[2]; check_only = true; }
    else if (strcmp(command, "run") == 0) { if (argc != 3) { usage(); return 2; } input = argv[2]; }
    else if (strcmp(command, "build") == 0) { if (argc != 3 && argc != 5) { usage(); return 2; } input = argv[2]; build = true; if (argc == 5) { if (strcmp(argv[3], "-o") != 0) { usage(); return 2; } output = argv[4]; } }
    else { input = argv[1]; }
    size_t length = 0; char *data = read_file(input, &length); if (!data) return 1;
    if (build) {
        bool owns_output = false;
        if (!output) { output = default_artifact_path(input); owns_output = true; }
        bool ok = write_artifact(output, data, length);
        free(data);
        if (ok) printf("built %s\n", output);
        if (owns_output) free((void *)output);
        return ok ? 0 : 1;
    }
    int result = execute_input(input, data, length, check_only); free(data); return result;
}
