#define _POSIX_C_SOURCE 200809L
#define _XOPEN_SOURCE 700

#include <ctype.h>
#include <errno.h>
#include <inttypes.h>
#include <limits.h>
#include <math.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

/* One native implementation, one checked AST, and one runtime semantic path. */

typedef struct Arena {
  void **items;
  size_t count;
  size_t capacity;
} Arena;
static Arena *arena_current;
static void fatal_oom(void) {
  fputs("kry: fatal[resource]: out of memory\n", stderr);
  exit(2);
}
static bool size_add(size_t a, size_t b, size_t *out) {
  if (b > SIZE_MAX - a)
    return false;
  *out = a + b;
  return true;
}
static bool size_mul(size_t a, size_t b, size_t *out) {
  if (a && b > SIZE_MAX / a)
    return false;
  *out = a * b;
  return true;
}
static void arena_track(void *ptr) {
  if (!arena_current || !ptr)
    return;
  if (arena_current->count == arena_current->capacity) {
    size_t next = arena_current->capacity ? arena_current->capacity * 2 : 64;
    void **grown = realloc(arena_current->items, next * sizeof(*grown));
    if (!grown)
      fatal_oom();
    arena_current->items = grown;
    arena_current->capacity = next;
  }
  arena_current->items[arena_current->count++] = ptr;
}
static void *aalloc(size_t size) {
  void *ptr = calloc(1, size ? size : 1);
  if (!ptr)
    fatal_oom();
  arena_track(ptr);
  return ptr;
}
static void *agrow(void *old, size_t old_count, size_t *capacity, size_t need,
                   size_t element_size) {
  if (need <= *capacity)
    return old;
  size_t next = *capacity ? *capacity * 2 : 8;
  while (next < need) {
    if (next > SIZE_MAX / 2)
      fatal_oom();
    next *= 2;
  }
  size_t bytes;
  if (!size_mul(next, element_size, &bytes))
    fatal_oom();
  void *grown = aalloc(bytes);
  if (old && old_count)
    memcpy(grown, old, old_count * element_size);
  *capacity = next;
  return grown;
}
static char *astrn(const char *text, size_t length) {
  size_t total;
  if (!size_add(length, 1, &total))
    fatal_oom();
  char *copy = aalloc(total);
  memcpy(copy, text, length);
  copy[length] = 0;
  return copy;
}
static char *astr(const char *text) { return astrn(text, strlen(text)); }
static void arena_free(Arena *arena) {
  for (size_t i = arena->count; i; i--)
    free(arena->items[i - 1]);
  free(arena->items);
  arena->items = NULL;
  arena->count = arena->capacity = 0;
}

typedef struct Source {
  char *name;
  char *text;
  size_t length;
} Source;
static Source *source_make(const char *name, const char *text, size_t length) {
  Source *s = aalloc(sizeof(*s));
  s->name = astr(name);
  s->text = astrn(text, length);
  s->length = length;
  return s;
}

typedef enum {
  ERR_LEX,
  ERR_PARSE,
  ERR_TYPE,
  ERR_RUNTIME,
  ERR_ARTIFACT,
  ERR_CLI,
  ERR_IO,
  ERR_RESOURCE
} ErrorKind;
static const char *error_name(ErrorKind kind) {
  switch (kind) {
  case ERR_LEX:
    return "lex";
  case ERR_PARSE:
    return "parse";
  case ERR_TYPE:
    return "type-mismatch";
  case ERR_RUNTIME:
    return "runtime";
  case ERR_ARTIFACT:
    return "artifact";
  case ERR_CLI:
    return "cli";
  case ERR_IO:
    return "io";
  case ERR_RESOURCE:
    return "resource";
  }
  return "error";
}
typedef struct {
  bool set;
  ErrorKind kind;
  Source *source;
  int line;
  int column;
  char message[768];
} Error;
static void error_set(Error *error, ErrorKind kind, Source *source, int line,
                      int column, const char *format, ...) {
  if (error->set)
    return;
  error->set = true;
  error->kind = kind;
  error->source = source;
  error->line = line < 1 ? 1 : line;
  error->column = column < 1 ? 1 : column;
  va_list args;
  va_start(args, format);
  vsnprintf(error->message, sizeof(error->message), format, args);
  va_end(args);
}
static void print_error(const Error *error) {
  const char *name = error->source ? error->source->name : "<input>";
  fprintf(stderr, "error[%s]: %s:%d:%d\n  %s\n", error_name(error->kind), name,
          error->line, error->column, error->message);
  if (!error->source)
    return;
  size_t start = 0;
  int line = 1;
  while (start < error->source->length && line < error->line) {
    if (error->source->text[start++] == '\n')
      line++;
  }
  size_t end = start;
  while (end < error->source->length && error->source->text[end] != '\n')
    end++;
  size_t length = end - start;
  if (length > 220)
    length = 220;
  if (length) {
    fprintf(stderr, "  %.*s\n  ", (int)length, error->source->text + start);
    for (int i = 1; i < error->column && i < 220; i++)
      fputc(error->source->text[start + (size_t)i - 1] == '\t' ? '\t' : ' ',
            stderr);
    fputs("^\n", stderr);
  }
}

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
  TOK_FAT_ARROW,
  TOK_DOUBLE_COLON,
  TOK_DOT,
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
  TOK_OR_OR
} TokenKind;
typedef struct {
  TokenKind kind;
  size_t start;
  size_t length;
  int line;
  int column;
  Source *source;
} Token;
typedef struct {
  Token *items;
  size_t count;
  size_t capacity;
  Source *source;
  Error error;
  int line;
  int column;
} Lexer;
static bool utf8_valid(const unsigned char *data, size_t length) {
  size_t i = 0;
  while (i < length) {
    unsigned char c = data[i];
    size_t width = c < 0x80                 ? 1
                   : c < 0xE0 && c >= 0xC2  ? 2
                   : c < 0xF0 && c >= 0xE0  ? 3
                   : c >= 0xF0 && c <= 0xF4 ? 4
                                            : 0;
    if (!width || width > length - i)
      return false;
    if (width == 3 && ((c == 0xE0 && data[i + 1] < 0xA0) ||
                       (c == 0xED && data[i + 1] >= 0xA0)))
      return false;
    if (width == 4 && ((c == 0xF0 && data[i + 1] < 0x90) ||
                       (c == 0xF4 && data[i + 1] >= 0x90)))
      return false;
    for (size_t j = 1; j < width; j++)
      if ((data[i + j] & 0xC0) != 0x80)
        return false;
    i += width;
  }
  return true;
}
static bool id_start(unsigned char c) {
  return isalpha(c) || c == '_' || c >= 0x80;
}
static bool id_part(unsigned char c) {
  return isalnum(c) || c == '_' || c >= 0x80;
}
static bool text_eq(Source *source, Token token, const char *text) {
  size_t length = strlen(text);
  return token.length == length &&
         !memcmp(source->text + token.start, text, length);
}
static TokenKind word_kind(Source *source, Token token) {
  static const struct {
    const char *word;
    TokenKind kind;
  } words[] = {{"fn", TOK_FN},
               {"let", TOK_LET},
               {"mut", TOK_MUT},
               {"if", TOK_IF},
               {"else", TOK_ELSE},
               {"while", TOK_WHILE},
               {"return", TOK_RETURN},
               {"break", TOK_BREAK},
               {"continue", TOK_CONTINUE},
               {"true", TOK_TRUE},
               {"false", TOK_FALSE},
               {"nil", TOK_NIL},
               {"pub", TOK_PUB},
               {"import", TOK_IMPORT},
               {"struct", TOK_STRUCT},
               {"enum", TOK_ENUM},
               {"match", TOK_MATCH}};
  for (size_t i = 0; i < sizeof(words) / sizeof(words[0]); i++)
    if (text_eq(source, token, words[i].word))
      return words[i].kind;
  return TOK_ID;
}
static void lex_push(Lexer *lexer, TokenKind kind, size_t start, size_t length,
                     int line, int column) {
  lexer->items = agrow(lexer->items, lexer->count, &lexer->capacity,
                       lexer->count + 1, sizeof(Token));
  lexer->items[lexer->count++] =
      (Token){kind, start, length, line, column, lexer->source};
}
static void lex_source(Lexer *lexer) {
  Source *source = lexer->source;
  if (!utf8_valid((const unsigned char *)source->text, source->length)) {
    error_set(&lexer->error, ERR_LEX, source, 1, 1,
              "source is not valid UTF-8");
    return;
  }
  size_t i = 0;
  lexer->line = 1;
  lexer->column = 1;
  while (i < source->length && !lexer->error.set) {
    unsigned char c = (unsigned char)source->text[i];
    if (c == ' ' || c == '\t' || c == '\r') {
      i++;
      lexer->column++;
      continue;
    }
    if (c == '\n') {
      i++;
      lexer->line++;
      lexer->column = 1;
      continue;
    }
    if (c == '/' && i + 1 < source->length && source->text[i + 1] == '/') {
      i += 2;
      lexer->column += 2;
      while (i < source->length && source->text[i] != '\n') {
        i++;
        lexer->column++;
      }
      continue;
    }
    if (c == '/' && i + 1 < source->length && source->text[i + 1] == '*') {
      int depth = 1;
      i += 2;
      lexer->column += 2;
      while (i < source->length && depth) {
        if (i + 1 < source->length && source->text[i] == '/' &&
            source->text[i + 1] == '*') {
          depth++;
          i += 2;
          lexer->column += 2;
        } else if (i + 1 < source->length && source->text[i] == '*' &&
                   source->text[i + 1] == '/') {
          depth--;
          i += 2;
          lexer->column += 2;
        } else if (source->text[i] == '\n') {
          i++;
          lexer->line++;
          lexer->column = 1;
        } else {
          i++;
          lexer->column++;
        }
      }
      if (depth)
        error_set(&lexer->error, ERR_LEX, source, lexer->line, lexer->column,
                  "unterminated block comment");
      continue;
    }
    int line = lexer->line, column = lexer->column;
    if (id_start(c)) {
      size_t start = i++;
      while (i < source->length && id_part((unsigned char)source->text[i])) {
        i++;
        lexer->column++;
      }
      Token t = {TOK_ID, start, i - start, line, column, source};
      t.kind = word_kind(source, t);
      lex_push(lexer, t.kind, t.start, t.length, t.line, t.column);
      lexer->column = column + (int)(i - start);
      continue;
    }
    if (isdigit(c)) {
      size_t start = i++;
      while (i < source->length && isdigit((unsigned char)source->text[i])) {
        i++;
        lexer->column++;
      }
      TokenKind kind = TOK_INT;
      if (i + 1 < source->length && source->text[i] == '.' &&
          isdigit((unsigned char)source->text[i + 1])) {
        kind = TOK_FLOAT;
        i++;
        lexer->column++;
        while (i < source->length && isdigit((unsigned char)source->text[i])) {
          i++;
          lexer->column++;
        }
      }
      lex_push(lexer, kind, start, i - start, line, column);
      lexer->column = column + (int)(i - start);
      continue;
    }
    if (c == '"') {
      size_t start = i++;
      lexer->column++;
      bool closed = false;
      while (i < source->length) {
        unsigned char current = (unsigned char)source->text[i];
        if (current == '\\') {
          if (i + 1 >= source->length)
            break;
          if (source->text[i + 1] == 'x' &&
              (i + 3 >= source->length ||
               !isxdigit((unsigned char)source->text[i + 2]) ||
               !isxdigit((unsigned char)source->text[i + 3]))) {
            error_set(&lexer->error, ERR_LEX, source, line, column,
                      "invalid hex escape in string literal");
            break;
          }
          size_t width = source->text[i + 1] == 'x' ? 4 : 2;
          i += width;
          lexer->column += (int)width;
        } else if (current == '"') {
          closed = true;
          break;
        } else if (current == '\n') {
          error_set(&lexer->error, ERR_LEX, source, lexer->line, lexer->column,
                    "newline in string literal");
          break;
        } else {
          i++;
          lexer->column++;
        }
      }
      if (!closed && !lexer->error.set)
        error_set(&lexer->error, ERR_LEX, source, line, column,
                  "unterminated string literal");
      if (!lexer->error.set) {
        i++;
        lexer->column++;
        lex_push(lexer, TOK_STRING, start, i - start, line, column);
      }
      continue;
    }
    TokenKind kind = TOK_EOF;
    size_t width = 1;
    switch (c) {
    case '(':
      kind = TOK_LPAREN;
      break;
    case ')':
      kind = TOK_RPAREN;
      break;
    case '{':
      kind = TOK_LBRACE;
      break;
    case '}':
      kind = TOK_RBRACE;
      break;
    case '[':
      kind = TOK_LBRACKET;
      break;
    case ']':
      kind = TOK_RBRACKET;
      break;
    case ',':
      kind = TOK_COMMA;
      break;
    case ';':
      kind = TOK_SEMICOLON;
      break;
    case '.':
      kind = TOK_DOT;
      break;
    case '+':
      kind = TOK_PLUS;
      break;
    case '*':
      kind = TOK_STAR;
      break;
    case '%':
      kind = TOK_PERCENT;
      break;
    case ':':
      if (i + 1 < source->length && source->text[i + 1] == ':') {
        kind = TOK_DOUBLE_COLON;
        width = 2;
      } else
        kind = TOK_COLON;
      break;
    case '-':
      if (i + 1 < source->length && source->text[i + 1] == '>') {
        kind = TOK_ARROW;
        width = 2;
      } else
        kind = TOK_MINUS;
      break;
    case '=':
      if (i + 1 < source->length && source->text[i + 1] == '>') {
        kind = TOK_FAT_ARROW;
        width = 2;
      } else if (i + 1 < source->length && source->text[i + 1] == '=') {
        kind = TOK_EQUAL_EQUAL;
        width = 2;
      } else
        kind = TOK_EQUAL;
      break;
    case '!':
      if (i + 1 < source->length && source->text[i + 1] == '=') {
        kind = TOK_BANG_EQUAL;
        width = 2;
      } else
        kind = TOK_BANG;
      break;
    case '<':
      if (i + 1 < source->length && source->text[i + 1] == '=') {
        kind = TOK_LESS_EQUAL;
        width = 2;
      } else
        kind = TOK_LESS;
      break;
    case '>':
      if (i + 1 < source->length && source->text[i + 1] == '=') {
        kind = TOK_GREATER_EQUAL;
        width = 2;
      } else
        kind = TOK_GREATER;
      break;
    case '&':
      if (i + 1 < source->length && source->text[i + 1] == '&') {
        kind = TOK_AND_AND;
        width = 2;
      } else
        error_set(&lexer->error, ERR_LEX, source, line, column,
                  "expected '&' in '&&'");
      break;
    case '|':
      if (i + 1 < source->length && source->text[i + 1] == '|') {
        kind = TOK_OR_OR;
        width = 2;
      } else
        error_set(&lexer->error, ERR_LEX, source, line, column,
                  "expected '|' in '||'");
      break;
    case '/':
      kind = TOK_SLASH;
      break;
    default:
      error_set(&lexer->error, ERR_LEX, source, line, column,
                "unexpected character '%c'", c);
      break;
    }
    if (!lexer->error.set) {
      lex_push(lexer, kind, i, width, line, column);
      i += width;
      lexer->column += (int)width;
    }
  }
  if (!lexer->error.set)
    lex_push(lexer, TOK_EOF, source->length, 0, lexer->line, lexer->column);
}

typedef enum {
  TYPE_ERROR,
  TYPE_VOID,
  TYPE_NIL,
  TYPE_INT,
  TYPE_FLOAT,
  TYPE_BOOL,
  TYPE_STRING,
  TYPE_BYTES,
  TYPE_ARRAY,
  TYPE_OPTION,
  TYPE_RESULT,
  TYPE_STRUCT,
  TYPE_ENUM
} TypeKind;
typedef struct StructDecl StructDecl;
typedef struct EnumDecl EnumDecl;
typedef struct Type Type;
struct Type {
  TypeKind kind;
  char *name;
  Type *first;
  Type *second;
  StructDecl *structure;
  EnumDecl *enumeration;
};
static Type t_error = {TYPE_ERROR, "<error>", NULL, NULL, NULL, NULL};
static Type t_void = {TYPE_VOID, "Void", NULL, NULL, NULL, NULL};
static Type t_nil = {TYPE_NIL, "Nil", NULL, NULL, NULL, NULL};
static Type t_int = {TYPE_INT, "Int", NULL, NULL, NULL, NULL};
static Type t_float = {TYPE_FLOAT, "Float", NULL, NULL, NULL, NULL};
static Type t_bool = {TYPE_BOOL, "Bool", NULL, NULL, NULL, NULL};
static Type t_string = {TYPE_STRING, "String", NULL, NULL, NULL, NULL};
static Type t_bytes = {TYPE_BYTES, "Bytes", NULL, NULL, NULL, NULL};
static Type *type_new(TypeKind kind, const char *name, Type *first,
                      Type *second) {
  Type *type = aalloc(sizeof(*type));
  type->kind = kind;
  type->name = astr(name);
  type->first = first;
  type->second = second;
  return type;
}
static Type *type_array(Type *first) {
  return type_new(TYPE_ARRAY, "Array", first, 0);
}
static Type *type_option(Type *first) {
  return type_new(TYPE_OPTION, "Option", first, 0);
}
static Type *type_result(Type *first, Type *second) {
  return type_new(TYPE_RESULT, "Result", first, second);
}
static bool type_equal(Type *a, Type *b) {
  if (!a || !b || a->kind != b->kind)
    return false;
  if (a->kind == TYPE_ARRAY)
    return (!a->first || !b->first) ? true : type_equal(a->first, b->first);
  if (a->kind == TYPE_OPTION)
    return type_equal(a->first, b->first);
  if (a->kind == TYPE_RESULT)
    return type_equal(a->first, b->first) && type_equal(a->second, b->second);
  if (a->kind == TYPE_STRUCT)
    return a->structure == b->structure;
  if (a->kind == TYPE_ENUM)
    return a->enumeration == b->enumeration;
  return true;
}
static bool type_numeric(Type *type) {
  return type && (type->kind == TYPE_INT || type->kind == TYPE_FLOAT);
}
static const char *type_label(Type *type) {
  return type ? type->name : "<unknown>";
}

typedef struct TypeSpec TypeSpec;
struct TypeSpec {
  char *name;
  TypeSpec **parameters;
  size_t parameter_count;
  Source *source;
  int line;
  int column;
};
typedef struct Expr Expr;
typedef struct Stmt Stmt;
typedef struct {
  char *name;
  Type *type;
  Source *source;
  int line;
  int column;
} Field;
struct StructDecl {
  char *name;
  bool is_public;
  Field *fields;
  size_t field_count;
  Source *source;
  int line;
  int column;
};
typedef struct {
  char *name;
  Source *source;
  int line;
  int column;
} Variant;
struct EnumDecl {
  char *name;
  bool is_public;
  Variant *variants;
  size_t variant_count;
  Source *source;
  int line;
  int column;
};
typedef struct {
  char *name;
  TypeSpec *type;
  Source *source;
  int line;
  int column;
} Parameter;
typedef enum {
  EX_INT,
  EX_FLOAT,
  EX_BOOL,
  EX_NIL,
  EX_STRING,
  EX_VAR,
  EX_UNARY,
  EX_BINARY,
  EX_CALL,
  EX_ARRAY,
  EX_INDEX,
  EX_FIELD,
  EX_STRUCT,
  EX_ENUM,
  EX_OPTION,
  EX_RESULT
} ExprKind;
struct Expr {
  ExprKind kind;
  Source *source;
  int line;
  int column;
  union {
    int64_t integer;
    double floating;
    bool boolean;
    char *string;
    char *name;
    struct {
      TokenKind op;
      Expr *operand;
    } unary;
    struct {
      TokenKind op;
      Expr *left;
      Expr *right;
    } binary;
    struct {
      char *name;
      Expr **args;
      size_t count;
    } call;
    struct {
      Expr **items;
      size_t count;
    } array;
    struct {
      Expr *base;
      Expr *index;
    } index;
    struct {
      Expr *base;
      char *field;
    } field;
    struct {
      char *name;
      char **fields;
      Expr **values;
      size_t count;
    } structure;
    struct {
      char *type_name;
      char *variant;
    } enumeration;
    struct {
      Expr *value;
      bool present;
    } option;
    struct {
      Expr *value;
      bool ok;
    } result;
  } as;
};
typedef enum {
  PAT_WILDCARD,
  PAT_NIL,
  PAT_BOOL,
  PAT_INT,
  PAT_STRING,
  PAT_ENUM,
  PAT_OPTION,
  PAT_RESULT
} PatternKind;
typedef struct {
  PatternKind kind;
  Source *source;
  int line;
  int column;
  bool boolean;
  int64_t integer;
  char *text;
  char *type_name;
  char *variant;
  char *binding;
  bool ok;
} Pattern;
typedef struct {
  Pattern pattern;
  Stmt **body;
  size_t body_count;
} MatchArm;
typedef enum {
  ST_LET,
  ST_EXPR,
  ST_ASSIGN,
  ST_IF,
  ST_WHILE,
  ST_RETURN,
  ST_BREAK,
  ST_CONTINUE,
  ST_MATCH
} StmtKind;
struct Stmt {
  StmtKind kind;
  /*
    ST_LET,
    ST_EXPR,
    ST_ASSIGN,
    ST_IF,
    ST_WHILE,
    ST_RETURN,
    ST_BREAK,
    ST_CONTINUE,
    ST_MATCH
  }*/
  Source *source;
  int line;
  int column;
  union {
    struct {
      char *name;
      bool is_mutable;
      TypeSpec *annotation;
      Expr *initializer;
    } let;
    Expr *expression;
    struct {
      Expr *target;
      Expr *value;
    } assign;
    struct {
      Expr *condition;
      Stmt **then_body;
      size_t then_count;
      Stmt **else_body;
      size_t else_count;
    } if_stmt;
    struct {
      Expr *condition;
      Stmt **body;
      size_t count;
    } while_stmt;
    Expr *return_value;
    struct {
      Expr *scrutinee;
      MatchArm *arms;
      size_t arm_count;
    } match_stmt;
  } as;
};
typedef struct Function {
  char *name;
  bool is_public;
  Parameter *parameters;
  size_t parameter_count;
  TypeSpec *return_type;
  Stmt **body;
  size_t body_count;
  Source *source;
  int line;
  int column;
} Function;
typedef struct {
  char *path;
  Source *source;
  int line;
  int column;
} ImportDecl;
typedef struct Program {
  Stmt **statements;
  size_t statement_count;
  Function **functions;
  size_t function_count;
  StructDecl **structures;
  size_t structure_count;
  EnumDecl **enumerations;
  size_t enumeration_count;
  ImportDecl *imports;
  size_t import_count;
} Program;

typedef struct {
  Token *tokens;
  size_t count;
  size_t current;
  Source *source;
  Error error;
} Parser;
static Expr *expr_new(Parser *p, ExprKind kind, Token token) {
  Expr *e = aalloc(sizeof(*e));
  e->kind = kind;
  e->source = token.source;
  e->line = token.line;
  e->column = token.column;
  (void)p;
  return e;
}
static Stmt *stmt_new(Parser *p, StmtKind kind, Token token) {
  Stmt *s = aalloc(sizeof(*s));
  s->kind = kind;
  s->source = token.source;
  s->line = token.line;
  s->column = token.column;
  (void)p;
  return s;
}
static Token *peek(Parser *p) { return &p->tokens[p->current]; }
static Token *prev(Parser *p) { return &p->tokens[p->current - 1]; }
static bool check_tok(Parser *p, TokenKind k) { return peek(p)->kind == k; }
static Token *advance_tok(Parser *p) {
  if (p->current + 1 < p->count)
    p->current++;
  return prev(p);
}
static bool match_tok(Parser *p, TokenKind k) {
  if (!check_tok(p, k))
    return false;
  advance_tok(p);
  return true;
}
static char *tok_text(Parser *p, Token t) {
  (void)p;
  return astrn(t.source->text + t.start, t.length);
}
static void parse_fail(Parser *p, Token *t, const char *format, ...) {
  if (p->error.set)
    return;
  p->error.set = true;
  p->error.kind = ERR_PARSE;
  p->error.source = t->source;
  p->error.line = t->line;
  p->error.column = t->column;
  va_list args;
  va_start(args, format);
  vsnprintf(p->error.message, sizeof(p->error.message), format, args);
  va_end(args);
}
static Token *expect_tok(Parser *p, TokenKind kind, const char *message) {
  if (check_tok(p, kind))
    return advance_tok(p);
  parse_fail(p, peek(p), "%s", message);
  return peek(p);
}
static TypeSpec *parse_type(Parser *p);
static Expr *parse_expression(Parser *p);
static Stmt *parse_statement(Parser *p);
static TypeSpec *parse_type(Parser *p) {
  Token t = *expect_tok(p, TOK_ID, "expected a type name");
  TypeSpec *spec = aalloc(sizeof(*spec));
  spec->name = tok_text(p, t);
  spec->source = t.source;
  spec->line = t.line;
  spec->column = t.column;
  if (match_tok(p, TOK_LBRACKET)) {
    size_t capacity = 0;
    if (!check_tok(p, TOK_RBRACKET))
      do {
        spec->parameters =
            agrow(spec->parameters, spec->parameter_count, &capacity,
                  spec->parameter_count + 1, sizeof(TypeSpec *));
        spec->parameters[spec->parameter_count++] = parse_type(p);
      } while (match_tok(p, TOK_COMMA));
    expect_tok(p, TOK_RBRACKET, "expected ']' after type parameters");
  }
  return spec;
}
static int hex_value(unsigned char c) {
  return isdigit(c) ? c - '0' : tolower(c) - 'a' + 10;
}
static char *decode_string(Parser *p, Token token) {
  const char *raw = token.source->text + token.start + 1;
  size_t length = token.length >= 2 ? token.length - 2 : 0;
  char *out = aalloc(length + 1);
  size_t used = 0;
  for (size_t i = 0; i < length; i++) {
    unsigned char c = (unsigned char)raw[i];
    if (c == '\\' && i + 1 < length) {
      unsigned char n = (unsigned char)raw[++i];
      if (n == 'n')
        c = '\n';
      else if (n == 'r')
        c = '\r';
      else if (n == 't')
        c = '\t';
      else if (n == '"')
        c = '"';
      else if (n == '\\')
        c = '\\';
      else if (n == 'x' && i + 2 < length) {
        unsigned char high = (unsigned char)raw[++i];
        unsigned char low = (unsigned char)raw[++i];
        c = (unsigned char)(hex_value(high) * 16 + hex_value(low));
      }
      else {
        parse_fail(p, &token, "unsupported escape sequence");
        return out;
      }
    }
    out[used++] = (char)c;
  }
  out[used] = 0;
  if (!utf8_valid((const unsigned char *)out, used))
    parse_fail(p, &token, "string literal is not valid UTF-8");
  return out;
}

static Expr *parse_primary(Parser *p) {
  Token token = *peek(p);
  if (match_tok(p, TOK_INT)) {
    char *text = tok_text(p, token);
    errno = 0;
    char *end = NULL;
    intmax_t value = strtoimax(text, &end, 10);
    Expr *e = expr_new(p, EX_INT, token);
    if (errno == ERANGE || !end || *end)
      parse_fail(p, &token,
                 "integer literal is outside the supported Int range");
    e->as.integer = (int64_t)value;
    return e;
  }
  if (match_tok(p, TOK_FLOAT)) {
    char *text = tok_text(p, token);
    errno = 0;
    char *end = NULL;
    double value = strtod(text, &end);
    Expr *e = expr_new(p, EX_FLOAT, token);
    if (errno == ERANGE || !end || *end || !isfinite(value))
      parse_fail(p, &token,
                 "floating-point literal must be finite and representable");
    e->as.floating = value;
    return e;
  }
  if (match_tok(p, TOK_STRING)) {
    Expr *e = expr_new(p, EX_STRING, token);
    e->as.string = decode_string(p, token);
    return e;
  }
  if (match_tok(p, TOK_TRUE) || match_tok(p, TOK_FALSE)) {
    Expr *e = expr_new(p, EX_BOOL, token);
    e->as.boolean = prev(p)->kind == TOK_TRUE;
    return e;
  }
  if (match_tok(p, TOK_NIL))
    return expr_new(p, EX_NIL, token);
  if (match_tok(p, TOK_ID)) {
    Expr *e = expr_new(p, EX_VAR, token);
    e->as.name = tok_text(p, token);
    if (match_tok(p, TOK_DOUBLE_COLON)) {
      Token variant =
          *expect_tok(p, TOK_ID, "expected an enum variant after '::'");
      Expr *value = expr_new(p, EX_ENUM, token);
      value->as.enumeration.type_name = e->as.name;
      value->as.enumeration.variant = tok_text(p, variant);
      return value;
    }
    if (check_tok(p, TOK_LBRACE) && p->current + 2 < p->count &&
        p->tokens[p->current + 1].kind == TOK_ID &&
        p->tokens[p->current + 2].kind == TOK_COLON) {
      advance_tok(p);
      Expr *value = expr_new(p, EX_STRUCT, token);
      value->as.structure.name = e->as.name;
      size_t field_capacity = 0, value_capacity = 0;
      while (!check_tok(p, TOK_RBRACE) && !check_tok(p, TOK_EOF) &&
             !p->error.set) {
        Token field = *expect_tok(p, TOK_ID, "expected a struct field name");
        expect_tok(p, TOK_COLON, "expected ':' after struct field name");
        value->as.structure.fields = agrow(
            value->as.structure.fields, value->as.structure.count,
            &field_capacity, value->as.structure.count + 1, sizeof(char *));
        value->as.structure.values = agrow(
            value->as.structure.values, value->as.structure.count,
            &value_capacity, value->as.structure.count + 1, sizeof(Expr *));
        value->as.structure.fields[value->as.structure.count] =
            tok_text(p, field);
        value->as.structure.values[value->as.structure.count] =
            parse_expression(p);
        value->as.structure.count++;
        if (!match_tok(p, TOK_COMMA))
          break;
      }
      expect_tok(p, TOK_RBRACE, "expected '}' after struct literal");
      return value;
    }
    return e;
  }
  if (match_tok(p, TOK_LPAREN)) {
    Expr *e = parse_expression(p);
    expect_tok(p, TOK_RPAREN, "expected ')' after expression");
    return e;
  }
  if (match_tok(p, TOK_LBRACKET)) {
    Expr *e = expr_new(p, EX_ARRAY, token);
    size_t capacity = 0;
    while (!check_tok(p, TOK_RBRACKET) && !check_tok(p, TOK_EOF) &&
           !p->error.set) {
      e->as.array.items = agrow(e->as.array.items, e->as.array.count, &capacity,
                                e->as.array.count + 1, sizeof(Expr *));
      e->as.array.items[e->as.array.count++] = parse_expression(p);
      if (!match_tok(p, TOK_COMMA))
        break;
    }
    expect_tok(p, TOK_RBRACKET, "expected ']' after array literal");
    return e;
  }
  parse_fail(p, &token, "expected an expression");
  return expr_new(p, EX_NIL, token);
}
static Expr *parse_unary(Parser *p) {
  if (match_tok(p, TOK_BANG) || match_tok(p, TOK_MINUS) ||
      match_tok(p, TOK_PLUS)) {
    Token token = *prev(p);
    Expr *e = expr_new(p, EX_UNARY, token);
    e->as.unary.op = token.kind;
    e->as.unary.operand = parse_unary(p);
    return e;
  }
  Expr *e = parse_primary(p);
  while (!p->error.set) {
    if (match_tok(p, TOK_LPAREN)) {
      if (e->kind != EX_VAR)
        parse_fail(p, prev(p), "only named functions can be called");
      Expr *call = expr_new(p, EX_CALL, *prev(p));
      call->as.call.name =
          e->kind == EX_VAR ? astr(e->as.name) : astr("<invalid>");
      size_t capacity = 0;
      while (!check_tok(p, TOK_RPAREN) && !check_tok(p, TOK_EOF) &&
             !p->error.set) {
        call->as.call.args =
            agrow(call->as.call.args, call->as.call.count, &capacity,
                  call->as.call.count + 1, sizeof(Expr *));
        call->as.call.args[call->as.call.count++] = parse_expression(p);
        if (!match_tok(p, TOK_COMMA))
          break;
      }
      expect_tok(p, TOK_RPAREN, "expected ')' after arguments");
      e = call;
    } else if (match_tok(p, TOK_LBRACKET)) {
      Expr *index = expr_new(p, EX_INDEX, *prev(p));
      index->as.index.base = e;
      index->as.index.index = parse_expression(p);
      expect_tok(p, TOK_RBRACKET, "expected ']' after index");
      e = index;
    } else if (match_tok(p, TOK_DOT)) {
      Token field = *expect_tok(p, TOK_ID, "expected a field name after '.'");
      Expr *access = expr_new(p, EX_FIELD, field);
      access->as.field.base = e;
      access->as.field.field = tok_text(p, field);
      e = access;
    } else
      break;
  }
  return e;
}
static int op_precedence(TokenKind kind) {
  switch (kind) {
  case TOK_OR_OR:
    return 1;
  case TOK_AND_AND:
    return 2;
  case TOK_EQUAL_EQUAL:
  case TOK_BANG_EQUAL:
    return 3;
  case TOK_LESS:
  case TOK_LESS_EQUAL:
  case TOK_GREATER:
  case TOK_GREATER_EQUAL:
    return 4;
  case TOK_PLUS:
  case TOK_MINUS:
    return 5;
  case TOK_STAR:
  case TOK_SLASH:
  case TOK_PERCENT:
    return 6;
  default:
    return 0;
  }
}
static Expr *parse_precedence(Parser *p, int minimum) {
  Expr *left = parse_unary(p);
  while (!p->error.set && op_precedence(peek(p)->kind) >= minimum &&
         op_precedence(peek(p)->kind) > 0) {
    Token op = *advance_tok(p);
    Expr *right = parse_precedence(p, op_precedence(op.kind) + 1);
    Expr *e = expr_new(p, EX_BINARY, op);
    e->as.binary.op = op.kind;
    e->as.binary.left = left;
    e->as.binary.right = right;
    left = e;
  }
  return left;
}
static Expr *parse_expression(Parser *p) { return parse_precedence(p, 1); }
static void consume_end(Parser *p) {
  while (match_tok(p, TOK_SEMICOLON)) {
  }
}
static void parse_block(Parser *p, Stmt ***items, size_t *count,
                        size_t *capacity) {
  expect_tok(p, TOK_LBRACE, "expected '{'");
  while (!check_tok(p, TOK_RBRACE) && !check_tok(p, TOK_EOF) && !p->error.set) {
    Stmt *s = parse_statement(p);
    if (s) {
      *items = agrow(*items, *count, capacity, *count + 1, sizeof(Stmt *));
      (*items)[(*count)++] = s;
    }
    consume_end(p);
  }
  expect_tok(p, TOK_RBRACE, "expected '}' after block");
}
static Stmt *parse_if(Parser *p) {
  Token token = *prev(p);
  Stmt *s = stmt_new(p, ST_IF, token);
  s->as.if_stmt.condition = parse_expression(p);
  size_t cap = 0;
  parse_block(p, &s->as.if_stmt.then_body, &s->as.if_stmt.then_count, &cap);
  if (match_tok(p, TOK_ELSE)) {
    if (match_tok(p, TOK_IF)) {
      Stmt *nested = parse_if(p);
      size_t else_cap = 0;
      s->as.if_stmt.else_body =
          agrow(s->as.if_stmt.else_body, 0, &else_cap, 1, sizeof(Stmt *));
      s->as.if_stmt.else_body[0] = nested;
      s->as.if_stmt.else_count = 1;
    } else {
      size_t else_cap = 0;
      parse_block(p, &s->as.if_stmt.else_body, &s->as.if_stmt.else_count,
                  &else_cap);
    }
  }
  return s;
}
static Pattern parse_pattern(Parser *p) {
  Token token = *peek(p);
  Pattern pattern = {
      PAT_WILDCARD, token.source, token.line, token.column, false, 0,
      NULL,         NULL,         NULL,       NULL,         false};
  if (match_tok(p, TOK_ID)) {
    char *name = tok_text(p, token);
    if (!strcmp(name, "_"))
      return pattern;
    if (!strcmp(name, "none")) {
      pattern.kind = PAT_OPTION;
      pattern.ok = false;
      return pattern;
    }
    if (!strcmp(name, "some") || !strcmp(name, "ok") || !strcmp(name, "err")) {
      pattern.kind = !strcmp(name, "some") ? PAT_OPTION : PAT_RESULT;
      pattern.ok = !strcmp(name, "some") || !strcmp(name, "ok");
      expect_tok(p, TOK_LPAREN, "expected '(' in option/result pattern");
      Token binding =
          *expect_tok(p, TOK_ID, "expected a binding name in pattern");
      pattern.binding = tok_text(p, binding);
      expect_tok(p, TOK_RPAREN, "expected ')' after pattern binding");
      return pattern;
    }
    if (match_tok(p, TOK_DOUBLE_COLON)) {
      pattern.kind = PAT_ENUM;
      pattern.type_name = name;
      Token variant = *expect_tok(p, TOK_ID, "expected an enum variant");
      pattern.variant = tok_text(p, variant);
      return pattern;
    }
    pattern.kind = PAT_ENUM;
    pattern.variant = name;
    return pattern;
  }
  if (match_tok(p, TOK_NIL)) {
    pattern.kind = PAT_NIL;
    return pattern;
  }
  if (match_tok(p, TOK_TRUE) || match_tok(p, TOK_FALSE)) {
    pattern.kind = PAT_BOOL;
    pattern.boolean = prev(p)->kind == TOK_TRUE;
    return pattern;
  }
  if (match_tok(p, TOK_INT)) {
    char *text = tok_text(p, token);
    pattern.kind = PAT_INT;
    pattern.integer = strtoimax(text, NULL, 10);
    return pattern;
  }
  if (match_tok(p, TOK_STRING)) {
    pattern.kind = PAT_STRING;
    pattern.text = decode_string(p, token);
    return pattern;
  }
  parse_fail(p, &token, "invalid match pattern");
  return pattern;
}
static Stmt *parse_match(Parser *p) {
  Token token = *prev(p);
  Stmt *s = stmt_new(p, ST_MATCH, token);
  s->as.match_stmt.scrutinee = parse_expression(p);
  expect_tok(p, TOK_LBRACE, "expected '{' after match expression");
  size_t capacity = 0;
  while (!check_tok(p, TOK_RBRACE) && !check_tok(p, TOK_EOF) && !p->error.set) {
    MatchArm arm = {parse_pattern(p), NULL, 0};
    expect_tok(p, TOK_FAT_ARROW, "expected '=>' after match pattern");
    size_t body_cap = 0;
    parse_block(p, &arm.body, &arm.body_count, &body_cap);
    s->as.match_stmt.arms =
        agrow(s->as.match_stmt.arms, s->as.match_stmt.arm_count, &capacity,
              s->as.match_stmt.arm_count + 1, sizeof(MatchArm));
    s->as.match_stmt.arms[s->as.match_stmt.arm_count++] = arm;
    consume_end(p);
  }
  expect_tok(p, TOK_RBRACE, "expected '}' after match arms");
  return s;
}
static Stmt *parse_statement(Parser *p) {
  Token token = *peek(p);
  if (match_tok(p, TOK_LET)) {
    Stmt *s = stmt_new(p, ST_LET, token);
    s->as.let.is_mutable = match_tok(p, TOK_MUT);
    Token name = *expect_tok(p, TOK_ID, "expected a binding name after 'let'");
    s->as.let.name = tok_text(p, name);
    if (match_tok(p, TOK_COLON))
      s->as.let.annotation = parse_type(p);
    expect_tok(p, TOK_EQUAL, "expected '=' in binding declaration");
    s->as.let.initializer = parse_expression(p);
    return s;
  }
  if (match_tok(p, TOK_IF))
    return parse_if(p);
  if (match_tok(p, TOK_WHILE)) {
    Stmt *s = stmt_new(p, ST_WHILE, token);
    s->as.while_stmt.condition = parse_expression(p);
    size_t cap = 0;
    parse_block(p, &s->as.while_stmt.body, &s->as.while_stmt.count, &cap);
    return s;
  }
  if (match_tok(p, TOK_MATCH))
    return parse_match(p);
  if (match_tok(p, TOK_RETURN)) {
    Stmt *s = stmt_new(p, ST_RETURN, token);
    if (!check_tok(p, TOK_RBRACE) && !check_tok(p, TOK_EOF) &&
        !check_tok(p, TOK_SEMICOLON))
      s->as.return_value = parse_expression(p);
    return s;
  }
  if (match_tok(p, TOK_BREAK))
    return stmt_new(p, ST_BREAK, token);
  if (match_tok(p, TOK_CONTINUE))
    return stmt_new(p, ST_CONTINUE, token);
  Expr *first = parse_expression(p);
  if (match_tok(p, TOK_EQUAL)) {
    Stmt *s = stmt_new(p, ST_ASSIGN, token);
    s->as.assign.target = first;
    s->as.assign.value = parse_expression(p);
    return s;
  }
  Stmt *s = stmt_new(p, ST_EXPR, token);
  s->as.expression = first;
  return s;
}
static Function *parse_function(Parser *p, bool is_public) {
  Token fn = *expect_tok(p, TOK_FN, "expected 'fn'");
  Token name = *expect_tok(p, TOK_ID, "expected a function name");
  Function *f = aalloc(sizeof(*f));
  f->name = tok_text(p, name);
  f->is_public = is_public;
  f->source = fn.source;
  f->line = fn.line;
  f->column = fn.column;
  expect_tok(p, TOK_LPAREN, "expected '(' after function name");
  size_t param_cap = 0;
  while (!check_tok(p, TOK_RPAREN) && !check_tok(p, TOK_EOF) && !p->error.set) {
    Token param = *expect_tok(p, TOK_ID, "expected a parameter name");
    expect_tok(p, TOK_COLON, "function parameters require an explicit type");
    Parameter value = {tok_text(p, param), parse_type(p), param.source,
                       param.line, param.column};
    f->parameters = agrow(f->parameters, f->parameter_count, &param_cap,
                          f->parameter_count + 1, sizeof(Parameter));
    f->parameters[f->parameter_count++] = value;
    if (!match_tok(p, TOK_COMMA))
      break;
  }
  expect_tok(p, TOK_RPAREN, "expected ')' after parameters");
  if (match_tok(p, TOK_ARROW))
    f->return_type = parse_type(p);
  else {
    f->return_type = aalloc(sizeof(TypeSpec));
    f->return_type->name = astr("Nil");
    f->return_type->source = fn.source;
    f->return_type->line = fn.line;
    f->return_type->column = fn.column;
  }
  size_t body_cap = 0;
  parse_block(p, &f->body, &f->body_count, &body_cap);
  return f;
}
static StructDecl *parse_struct(Parser *p, bool is_public) {
  Token token = *expect_tok(p, TOK_STRUCT, "expected 'struct'");
  Token name = *expect_tok(p, TOK_ID, "expected a struct name");
  StructDecl *d = aalloc(sizeof(*d));
  d->name = tok_text(p, name);
  d->is_public = is_public;
  d->source = token.source;
  d->line = token.line;
  d->column = token.column;
  expect_tok(p, TOK_LBRACE, "expected '{' after struct name");
  size_t cap = 0;
  while (!check_tok(p, TOK_RBRACE) && !check_tok(p, TOK_EOF) && !p->error.set) {
    Token field = *expect_tok(p, TOK_ID, "expected a struct field name");
    expect_tok(p, TOK_COLON, "expected ':' after field name");
    Field value = {tok_text(p, field), NULL, field.source, field.line,
                   field.column};
    TypeSpec *spec = parse_type(p);
    value.type = (Type *)spec;
    d->fields = agrow(d->fields, d->field_count, &cap, d->field_count + 1,
                      sizeof(Field));
    d->fields[d->field_count++] = value;
    if (!match_tok(p, TOK_COMMA))
      consume_end(p);
  }
  expect_tok(p, TOK_RBRACE, "expected '}' after struct declaration");
  return d;
}
static EnumDecl *parse_enum(Parser *p, bool is_public) {
  Token token = *expect_tok(p, TOK_ENUM, "expected 'enum'");
  Token name = *expect_tok(p, TOK_ID, "expected an enum name");
  EnumDecl *d = aalloc(sizeof(*d));
  d->name = tok_text(p, name);
  d->is_public = is_public;
  d->source = token.source;
  d->line = token.line;
  d->column = token.column;
  expect_tok(p, TOK_LBRACE, "expected '{' after enum name");
  size_t cap = 0;
  while (!check_tok(p, TOK_RBRACE) && !check_tok(p, TOK_EOF) && !p->error.set) {
    Token variant = *expect_tok(p, TOK_ID, "expected an enum variant");
    Variant value = {tok_text(p, variant), variant.source, variant.line,
                     variant.column};
    d->variants = agrow(d->variants, d->variant_count, &cap,
                        d->variant_count + 1, sizeof(Variant));
    d->variants[d->variant_count++] = value;
    if (!match_tok(p, TOK_COMMA))
      consume_end(p);
  }
  expect_tok(p, TOK_RBRACE, "expected '}' after enum declaration");
  return d;
}
static Program *parse_program(Source *source, Error *error) {
  Lexer lexer = {NULL, 0, 0, source, {0}, 1, 1};
  lex_source(&lexer);
  if (lexer.error.set) {
    *error = lexer.error;
    return NULL;
  }
  Parser p = {lexer.items, lexer.count, 0, source, {0}};
  Program *program = aalloc(sizeof(*program));
  size_t statement_cap = 0, function_cap = 0, struct_cap = 0, enum_cap = 0,
         import_cap = 0;
  while (!check_tok(&p, TOK_EOF) && !p.error.set) {
    bool is_public = match_tok(&p, TOK_PUB);
    if (check_tok(&p, TOK_FN)) {
      program->functions =
          agrow(program->functions, program->function_count, &function_cap,
                program->function_count + 1, sizeof(Function *));
      program->functions[program->function_count++] =
          parse_function(&p, is_public);
    } else if (check_tok(&p, TOK_STRUCT)) {
      program->structures =
          agrow(program->structures, program->structure_count, &struct_cap,
                program->structure_count + 1, sizeof(StructDecl *));
      program->structures[program->structure_count++] =
          parse_struct(&p, is_public);
    } else if (check_tok(&p, TOK_ENUM)) {
      program->enumerations =
          agrow(program->enumerations, program->enumeration_count, &enum_cap,
                program->enumeration_count + 1, sizeof(EnumDecl *));
      program->enumerations[program->enumeration_count++] =
          parse_enum(&p, is_public);
    } else if (match_tok(&p, TOK_IMPORT)) {
      Token path =
          *expect_tok(&p, TOK_STRING, "import expects a quoted module path");
      ImportDecl value = {decode_string(&p, path), path.source, path.line,
                          path.column};
      program->imports =
          agrow(program->imports, program->import_count, &import_cap,
                program->import_count + 1, sizeof(ImportDecl));
      program->imports[program->import_count++] = value;
      consume_end(&p);
    } else if (is_public)
      parse_fail(&p, prev(&p),
                 "'pub' must be followed by a function, struct, or enum");
    else {
      Stmt *s = parse_statement(&p);
      if (s) {
        program->statements =
            agrow(program->statements, program->statement_count, &statement_cap,
                  program->statement_count + 1, sizeof(Stmt *));
        program->statements[program->statement_count++] = s;
      }
      consume_end(&p);
    }
  }
  if (p.error.set) {
    *error = p.error;
    return NULL;
  }
  return program;
}

typedef struct Binding {
  char *name;
  Type *type;
  bool is_mutable;
  struct Binding *next;
} Binding;
typedef struct Scope {
  Binding *bindings;
  struct Scope *parent;
} Scope;
static Binding *scope_lookup(Scope *scope, const char *name) {
  for (Scope *s = scope; s; s = s->parent)
    for (Binding *b = s->bindings; b; b = b->next)
      if (!strcmp(b->name, name))
        return b;
  return NULL;
}
static Binding *scope_lookup_local(Scope *scope, const char *name) {
  for (Binding *b = scope->bindings; b; b = b->next)
    if (!strcmp(b->name, name))
      return b;
  return NULL;
}
static bool scope_define(Scope *scope, const char *name, Type *type,
                         bool is_mutable, Error *error, Source *source,
                         int line, int column) {
  if (scope_lookup_local(scope, name)) {
    error_set(error, ERR_TYPE, source, line, column,
              "binding '%s' is already defined in this scope", name);
    return false;
  }
  Binding *b = aalloc(sizeof(*b));
  b->name = astr(name);
  b->type = type;
  b->is_mutable = is_mutable;
  b->next = scope->bindings;
  scope->bindings = b;
  return true;
}
static StructDecl *find_struct(Program *program, const char *name) {
  for (size_t i = 0; i < program->structure_count; i++)
    if (!strcmp(program->structures[i]->name, name))
      return program->structures[i];
  return NULL;
}
static EnumDecl *find_enum(Program *program, const char *name) {
  for (size_t i = 0; i < program->enumeration_count; i++)
    if (!strcmp(program->enumerations[i]->name, name))
      return program->enumerations[i];
  return NULL;
}
static Function *find_function(Program *program, const char *name) {
  for (size_t i = 0; i < program->function_count; i++)
    if (!strcmp(program->functions[i]->name, name))
      return program->functions[i];
  return NULL;
}
static Type *resolve_type(Program *program, TypeSpec *spec, Error *error) {
  if (!spec)
    return &t_nil;
  if (!strcmp(spec->name, "Void") && !spec->parameter_count)
    return &t_void;
  if (!strcmp(spec->name, "Nil") && !spec->parameter_count)
    return &t_nil;
  if (!strcmp(spec->name, "Int") && !spec->parameter_count)
    return &t_int;
  if (!strcmp(spec->name, "Float") && !spec->parameter_count)
    return &t_float;
  if (!strcmp(spec->name, "Bool") && !spec->parameter_count)
    return &t_bool;
  if (!strcmp(spec->name, "String") && !spec->parameter_count)
    return &t_string;
  if (!strcmp(spec->name, "Bytes") && !spec->parameter_count)
    return &t_bytes;
  if (!strcmp(spec->name, "Array") && spec->parameter_count == 0)
    return type_array(NULL);
  if (!strcmp(spec->name, "Array") && spec->parameter_count == 1)
    return type_array(resolve_type(program, spec->parameters[0], error));
  if (!strcmp(spec->name, "Option") && spec->parameter_count == 1)
    return type_option(resolve_type(program, spec->parameters[0], error));
  if (!strcmp(spec->name, "Result") && spec->parameter_count == 2)
    return type_result(resolve_type(program, spec->parameters[0], error),
                       resolve_type(program, spec->parameters[1], error));
  StructDecl *structure = find_struct(program, spec->name);
  if (structure && !spec->parameter_count) {
    Type *type = type_new(TYPE_STRUCT, structure->name, NULL, NULL);
    type->structure = structure;
    return type;
  }
  EnumDecl *enumeration = find_enum(program, spec->name);
  if (enumeration && !spec->parameter_count) {
    Type *type = type_new(TYPE_ENUM, enumeration->name, NULL, NULL);
    type->enumeration = enumeration;
    return type;
  }
  error_set(error, ERR_TYPE, spec->source, spec->line, spec->column,
            "unknown or malformed type '%s'", spec->name);
  return &t_error;
}

typedef enum {
  B_PRINT,
  B_PRINTLN,
  B_LEN,
  B_BYTES,
  B_STRING_TO_BYTES,
  B_BYTES_TO_STRING,
  B_ARRAY_PUSH,
  B_INT,
  B_FLOAT,
  B_STR,
  B_BOOL,
  B_ASSERT,
  B_ASSERT_EQ,
  B_ABS,
  B_SQRT,
  B_SOME,
  B_NONE,
  B_OK,
  B_ERR
} BuiltinId;
typedef struct {
  const char *name;
  int arity;
  BuiltinId id;
  const char *description;
} BuiltinSpec;
static const BuiltinSpec builtins[] = {
    {"print", 1, B_PRINT, "Write one value without a newline."},
    {"println", 1, B_PRINTLN, "Write one value followed by a newline."},
    {"len", 1, B_LEN, "Count String code points, Array elements, or Bytes."},
    {"bytes", 1, B_BYTES, "Convert Array[Int] octets to Bytes."},
    {"string_to_bytes", 1, B_STRING_TO_BYTES,
     "Encode a String as UTF-8 Bytes."},
    {"bytes_to_string", 1, B_BYTES_TO_STRING,
     "Decode valid UTF-8 Bytes as String."},
    {"array_push", 2, B_ARRAY_PUSH,
     "Return an Array with one element appended."},
    {"int", 1, B_INT,
     "Explicitly convert Int, Float, Bool, or a complete decimal String."},
    {"float", 1, B_FLOAT,
     "Explicitly convert Int, Float, or a complete decimal String."},
    {"str", 1, B_STR, "Convert any value to its deterministic display String."},
    {"bool", 1, B_BOOL, "Explicitly convert a scalar or collection to Bool."},
    {"assert", 1, B_ASSERT, "Require a Bool condition."},
    {"assert_eq", 2, B_ASSERT_EQ, "Require two equal values."},
    {"abs", 1, B_ABS, "Checked absolute value for Int or Float."},
    {"sqrt", 1, B_SQRT, "Compute a finite non-negative square root."},
    {"some", 1, B_SOME, "Construct Option[T] containing a value."},
    {"none", 0, B_NONE, "Construct an empty Option[T]."},
    {"ok", 1, B_OK, "Construct Result[T, E] containing a success value."},
    {"err", 1, B_ERR, "Construct Result[T, E] containing an error value."}};
static const size_t builtin_count = sizeof(builtins) / sizeof(builtins[0]);
static const BuiltinSpec *builtin_find(const char *name) {
  for (size_t i = 0; i < builtin_count; i++)
    if (!strcmp(builtins[i].name, name))
      return &builtins[i];
  return NULL;
}

static Type *check_expr(Program *program, Scope *scope, Expr *expr,
                        Type *expected, Error *error);
static bool check_statements(Program *program, Scope *scope, Stmt **statements,
                             size_t count, Type *return_type, int loop_depth,
                             bool in_function, Error *error, bool *returns);
static Type *check_call(Program *program, Scope *scope, Expr *expr,
                        Type *expected, Error *error) {
  const char *name = expr->as.call.name;
  const BuiltinSpec *builtin = builtin_find(name);
  if (builtin) {
    if ((int)expr->as.call.count != builtin->arity) {
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "builtin '%s' expects %d argument(s), got %zu", name,
                builtin->arity, expr->as.call.count);
      return &t_error;
    }
    Type *first =
        expr->as.call.count
            ? check_expr(program, scope, expr->as.call.args[0], NULL, error)
            : &t_nil;
    if (error->set)
      return &t_error;
    switch (builtin->id) {
    case B_PRINT:
    case B_PRINTLN:
      return &t_nil;
    case B_LEN:
      if (first->kind != TYPE_STRING && first->kind != TYPE_ARRAY &&
          first->kind != TYPE_BYTES) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "len expects String, Array[T], or Bytes; found %s",
                  type_label(first));
        return &t_error;
      }
      return &t_int;
    case B_BYTES:
      if (first->kind != TYPE_ARRAY || !first->first ||
          first->first->kind != TYPE_INT) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "bytes expects Array[Int]");
        return &t_error;
      }
      return &t_bytes;
    case B_STRING_TO_BYTES:
      if (!type_equal(first, &t_string)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "string_to_bytes expects String");
        return &t_error;
      }
      return &t_bytes;
    case B_BYTES_TO_STRING:
      if (!type_equal(first, &t_bytes)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "bytes_to_string expects Bytes");
        return &t_error;
      }
      return &t_string;
    case B_ARRAY_PUSH: {
      if (first->kind != TYPE_ARRAY) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "array_push expects Array[T] as its first argument");
        return &t_error;
      }
      Type *value = check_expr(program, scope, expr->as.call.args[1],
                               first->first, error);
      if (!first->first)
        return type_array(value);
      if (!error->set && !type_equal(value, first->first))
        error_set(error, ERR_TYPE, expr->as.call.args[1]->source,
                  expr->as.call.args[1]->line, expr->as.call.args[1]->column,
                  "array_push element expected %s, found %s",
                  type_label(first->first), type_label(value));
      return first;
    }
    case B_INT:
      if (first->kind != TYPE_INT && first->kind != TYPE_FLOAT &&
          first->kind != TYPE_BOOL && first->kind != TYPE_STRING) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "int accepts Int, Float, Bool, or String");
        return &t_error;
      }
      return &t_int;
    case B_FLOAT:
      if (first->kind != TYPE_INT && first->kind != TYPE_FLOAT &&
          first->kind != TYPE_STRING) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "float accepts Int, Float, or String");
        return &t_error;
      }
      return &t_float;
    case B_STR:
      return &t_string;
    case B_BOOL:
      return &t_bool;
    case B_ASSERT:
      if (!type_equal(first, &t_bool)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "assert expects Bool");
        return &t_error;
      }
      return &t_nil;
    case B_ASSERT_EQ: {
      Type *second =
          check_expr(program, scope, expr->as.call.args[1], first, error);
      if (!error->set && !type_equal(first, second))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "assert_eq arguments must have the same type");
      return &t_nil;
    }
    case B_ABS:
      if (!type_numeric(first)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "abs expects Int or Float");
        return &t_error;
      }
      return first;
    case B_SQRT:
      if (!type_numeric(first)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "sqrt expects Int or Float");
        return &t_error;
      }
      return &t_float;
    case B_SOME:
      return type_option(first);
    case B_NONE:
      if (expected && expected->kind == TYPE_OPTION)
        return expected;
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "none requires an Option[T] context");
      return &t_error;
    case B_OK:
      return type_result(first, expected && expected->kind == TYPE_RESULT
                                    ? expected->second
                                    : &t_nil);
    case B_ERR:
      return type_result(
          expected && expected->kind == TYPE_RESULT ? expected->first : &t_nil,
          first);
    }
  }
  Function *function = find_function(program, name);
  if (!function) {
    error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
              "unknown function '%s'", name);
    return &t_error;
  }
  if (expr->as.call.count != function->parameter_count) {
    error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
              "function '%s' expects %zu argument(s), got %zu", name,
              function->parameter_count, expr->as.call.count);
    return &t_error;
  }
  for (size_t i = 0; i < function->parameter_count; i++) {
    Type *parameter =
        resolve_type(program, function->parameters[i].type, error);
    Type *argument =
        check_expr(program, scope, expr->as.call.args[i], parameter, error);
    if (!error->set && !type_equal(parameter, argument))
      error_set(error, ERR_TYPE, expr->as.call.args[i]->source,
                expr->as.call.args[i]->line, expr->as.call.args[i]->column,
                "argument %zu to '%s' expected %s, found %s", i + 1, name,
                type_label(parameter), type_label(argument));
  }
  return resolve_type(program, function->return_type, error);
}
static Type *check_expr(Program *program, Scope *scope, Expr *expr,
                        Type *expected, Error *error) {
  if (error->set)
    return &t_error;
  Type *left, *right;
  switch (expr->kind) {
  case EX_INT:
    return &t_int;
  case EX_FLOAT:
    return &t_float;
  case EX_BOOL:
    return &t_bool;
  case EX_NIL:
    return &t_nil;
  case EX_STRING:
    return &t_string;
  case EX_VAR: {
    Binding *b = scope_lookup(scope, expr->as.name);
    if (!b) {
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "unknown variable '%s'", expr->as.name);
      return &t_error;
    }
    return b->type;
  }
  case EX_ARRAY: {
    if (!expr->as.array.count) {
      if (expected && expected->kind == TYPE_ARRAY)
        return expected;
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "empty array literals require an Array[T] annotation");
      return &t_error;
    }
    Type *element = check_expr(
        program, scope, expr->as.array.items[0],
        expected && expected->kind == TYPE_ARRAY ? expected->first : NULL,
        error);
    for (size_t i = 1; i < expr->as.array.count; i++) {
      Type *item =
          check_expr(program, scope, expr->as.array.items[i], element, error);
      if (!error->set && !type_equal(element, item))
        error_set(error, ERR_TYPE, expr->as.array.items[i]->source,
                  expr->as.array.items[i]->line,
                  expr->as.array.items[i]->column,
                  "array elements must have one homogeneous type; expected %s, "
                  "found %s",
                  type_label(element), type_label(item));
    }
    return type_array(element);
  }
  case EX_UNARY: {
    Type *operand =
        check_expr(program, scope, expr->as.unary.operand, NULL, error);
    if (expr->as.unary.op == TOK_BANG) {
      if (!type_equal(operand, &t_bool))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "'!' expects Bool, found %s", type_label(operand));
      return &t_bool;
    }
    if (!type_numeric(operand))
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "unary numeric operator expects Int or Float");
    return operand;
  }
  case EX_BINARY:
    left = check_expr(program, scope, expr->as.binary.left, NULL, error);
    if (error->set)
      return &t_error;
    if (expr->as.binary.op == TOK_AND_AND || expr->as.binary.op == TOK_OR_OR) {
      if (!type_equal(left, &t_bool))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "boolean operators require Bool operands");
      right = check_expr(program, scope, expr->as.binary.right, &t_bool, error);
      if (!error->set && !type_equal(right, &t_bool))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "boolean operators require Bool operands");
      return &t_bool;
    }
    right = check_expr(program, scope, expr->as.binary.right, NULL, error);
    if (error->set)
      return &t_error;
    if (expr->as.binary.op == TOK_EQUAL_EQUAL ||
        expr->as.binary.op == TOK_BANG_EQUAL) {
      if (!type_equal(left, right))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "equality operands must have the same type");
      return &t_bool;
    }
    if (expr->as.binary.op == TOK_LESS ||
        expr->as.binary.op == TOK_LESS_EQUAL ||
        expr->as.binary.op == TOK_GREATER ||
        expr->as.binary.op == TOK_GREATER_EQUAL) {
      if (!type_numeric(left) || !type_equal(left, right))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "ordered comparisons require matching numeric types");
      return &t_bool;
    }
    if (expr->as.binary.op == TOK_PLUS &&
        ((type_equal(left, &t_string) && type_equal(right, &t_string)) ||
         (left->kind == TYPE_ARRAY && type_equal(left, right)) ||
         (type_equal(left, &t_bytes) && type_equal(right, &t_bytes))))
      return left;
    if (expr->as.binary.op == TOK_PERCENT &&
        (left->kind != TYPE_INT || right->kind != TYPE_INT))
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "remainder requires Int operands");
    else if (!type_numeric(left) || !type_equal(left, right))
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "operator requires matching numeric types or supported "
                "concatenation operands");
    return left;
  case EX_CALL:
    return check_call(program, scope, expr, expected, error);
  case EX_INDEX: {
    Type *base = check_expr(program, scope, expr->as.index.base, NULL, error);
    Type *index =
        check_expr(program, scope, expr->as.index.index, &t_int, error);
    if (!type_equal(index, &t_int)) {
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "index must be Int");
      return &t_error;
    }
    if (base->kind == TYPE_ARRAY)
      return base->first;
    if (base->kind == TYPE_STRING)
      return &t_string;
    if (base->kind == TYPE_BYTES)
      return &t_int;
    error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
              "indexing requires String, Array[T], or Bytes");
    return &t_error;
  }
  case EX_FIELD: {
    Type *base = check_expr(program, scope, expr->as.field.base, NULL, error);
    if (base->kind != TYPE_STRUCT) {
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "field access requires a struct value");
      return &t_error;
    }
    for (size_t i = 0; i < base->structure->field_count; i++)
      if (!strcmp(base->structure->fields[i].name, expr->as.field.field))
        return base->structure->fields[i].type;
    error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
              "struct '%s' has no field '%s'", base->structure->name,
              expr->as.field.field);
    return &t_error;
  }
  case EX_STRUCT: {
    StructDecl *decl = find_struct(program, expr->as.structure.name);
    if (!decl) {
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "unknown struct '%s'", expr->as.structure.name);
      return &t_error;
    }
    if (expr->as.structure.count != decl->field_count) {
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "struct '%s' requires exactly %zu fields", decl->name,
                decl->field_count);
      return &t_error;
    }
    for (size_t i = 0; i < expr->as.structure.count; i++) {
      size_t field = decl->field_count;
      for (size_t j = 0; j < decl->field_count; j++)
        if (!strcmp(expr->as.structure.fields[i], decl->fields[j].name))
          field = j;
      if (field == decl->field_count) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "unknown field '%s' in struct '%s'",
                  expr->as.structure.fields[i], decl->name);
        return &t_error;
      }
      Type *actual = check_expr(program, scope, expr->as.structure.values[i],
                                decl->fields[field].type, error);
      if (!error->set && !type_equal(actual, decl->fields[field].type))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "field '%s' expected %s, found %s", decl->fields[field].name,
                  type_label(decl->fields[field].type), type_label(actual));
    }
    Type *type = type_new(TYPE_STRUCT, decl->name, NULL, NULL);
    type->structure = decl;
    return type;
  }
  case EX_ENUM: {
    EnumDecl *decl = find_enum(program, expr->as.enumeration.type_name);
    if (!decl) {
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "unknown enum '%s'", expr->as.enumeration.type_name);
      return &t_error;
    }
    bool found = false;
    for (size_t i = 0; i < decl->variant_count; i++)
      if (!strcmp(decl->variants[i].name, expr->as.enumeration.variant))
        found = true;
    if (!found)
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "enum '%s' has no variant '%s'", decl->name,
                expr->as.enumeration.variant);
    Type *type = type_new(TYPE_ENUM, decl->name, NULL, NULL);
    type->enumeration = decl;
    return type;
  }
  case EX_OPTION: {
    Type *inner =
        check_expr(program, scope, expr->as.option.value, NULL, error);
    return type_option(inner);
  }
  case EX_RESULT: {
    Type *inner =
        check_expr(program, scope, expr->as.result.value, NULL, error);
    return type_result(inner, &t_nil);
  }
  }
  return &t_error;
}

static bool check_assignment(Program *program, Scope *scope, Expr *target,
                             Type *value, Error *error) {
  (void)program;
  if (target->kind != EX_VAR) {
    error_set(error, ERR_TYPE, target->source, target->line, target->column,
              "only variable bindings can be assigned");
    return false;
  }
  Binding *binding = scope_lookup(scope, target->as.name);
  if (!binding) {
    error_set(error, ERR_TYPE, target->source, target->line, target->column,
              "unknown variable '%s'", target->as.name);
    return false;
  }
  if (!binding->is_mutable) {
    error_set(
        error, ERR_TYPE, target->source, target->line, target->column,
        "cannot assign to immutable binding '%s'; declare it with 'let mut'",
        target->as.name);
    return false;
  }
  if (!type_equal(binding->type, value)) {
    error_set(error, ERR_TYPE, target->source, target->line, target->column,
              "assignment to '%s' expected %s, found %s", target->as.name,
              type_label(binding->type), type_label(value));
    return false;
  }
  return true;
}
static bool check_pattern(Type *scrutinee, Pattern *pattern, Scope *scope,
                          Error *error) {
  if (pattern->kind == PAT_WILDCARD)
    return true;
  if (pattern->kind == PAT_NIL) {
    if (scrutinee->kind != TYPE_NIL && scrutinee->kind != TYPE_OPTION &&
        scrutinee->kind != TYPE_RESULT)
      error_set(error, ERR_TYPE, pattern->source, pattern->line,
                pattern->column, "nil pattern does not match %s",
                type_label(scrutinee));
    return !error->set;
  }
  if (pattern->kind == PAT_BOOL && scrutinee->kind != TYPE_BOOL) {
    error_set(error, ERR_TYPE, pattern->source, pattern->line, pattern->column,
              "boolean pattern requires Bool");
    return false;
  }
  if (pattern->kind == PAT_INT && scrutinee->kind != TYPE_INT) {
    error_set(error, ERR_TYPE, pattern->source, pattern->line, pattern->column,
              "integer pattern requires Int");
    return false;
  }
  if (pattern->kind == PAT_STRING && scrutinee->kind != TYPE_STRING) {
    error_set(error, ERR_TYPE, pattern->source, pattern->line, pattern->column,
              "string pattern requires String");
    return false;
  }
  if (pattern->kind == PAT_ENUM) {
    if (scrutinee->kind != TYPE_ENUM) {
      error_set(error, ERR_TYPE, pattern->source, pattern->line,
                pattern->column, "enum pattern requires an enum value");
      return false;
    }
    if (pattern->type_name &&
        strcmp(pattern->type_name, scrutinee->enumeration->name)) {
      error_set(error, ERR_TYPE, pattern->source, pattern->line,
                pattern->column, "pattern belongs to enum '%s', found '%s'",
                pattern->type_name, scrutinee->enumeration->name);
      return false;
    }
    bool found = false;
    for (size_t i = 0; i < scrutinee->enumeration->variant_count; i++)
      if (!strcmp(scrutinee->enumeration->variants[i].name, pattern->variant))
        found = true;
    if (!found)
      error_set(error, ERR_TYPE, pattern->source, pattern->line,
                pattern->column, "enum '%s' has no variant '%s'",
                scrutinee->enumeration->name, pattern->variant);
    return found;
  }
  if (pattern->kind == PAT_OPTION) {
    if (scrutinee->kind != TYPE_OPTION) {
      error_set(error, ERR_TYPE, pattern->source, pattern->line,
                pattern->column, "option pattern requires Option[T]");
      return false;
    }
    if (pattern->binding)
      scope_define(scope, pattern->binding, scrutinee->first, false, error,
                   pattern->source, pattern->line, pattern->column);
    return true;
  }
  if (pattern->kind == PAT_RESULT) {
    if (scrutinee->kind != TYPE_RESULT) {
      error_set(error, ERR_TYPE, pattern->source, pattern->line,
                pattern->column, "result pattern requires Result[T, E]");
      return false;
    }
    if (pattern->binding)
      scope_define(scope, pattern->binding,
                   pattern->ok ? scrutinee->first : scrutinee->second, false,
                   error, pattern->source, pattern->line, pattern->column);
    return true;
  }
  return true;
}
static bool check_statements(Program *program, Scope *scope, Stmt **statements,
                             size_t count, Type *return_type, int loop_depth,
                             bool in_function, Error *error, bool *returns) {
  bool did_return = false;
  for (size_t i = 0; i < count && !error->set; i++) {
    Stmt *s = statements[i];
    switch (s->kind) {
    case ST_LET: {
      Type *declared = s->as.let.annotation
                           ? resolve_type(program, s->as.let.annotation, error)
                           : NULL;
      Type *actual =
          check_expr(program, scope, s->as.let.initializer, declared, error);
      if (!declared)
        declared = actual;
      else if (!type_equal(declared, actual) && !error->set)
        error_set(error, ERR_TYPE, s->source, s->line, s->column,
                  "binding '%s' declared %s but initializer has type %s",
                  s->as.let.name, type_label(declared), type_label(actual));
      scope_define(scope, s->as.let.name, declared, s->as.let.is_mutable, error,
                   s->source, s->line, s->column);
      break;
    }
    case ST_EXPR:
      (void)check_expr(program, scope, s->as.expression, NULL, error);
      break;
    case ST_ASSIGN: {
      Type *value = check_expr(program, scope, s->as.assign.value, NULL, error);
      if (!error->set)
        check_assignment(program, scope, s->as.assign.target, value, error);
      break;
    }
    case ST_IF: {
      Type *condition =
          check_expr(program, scope, s->as.if_stmt.condition, &t_bool, error);
      if (!error->set && !type_equal(condition, &t_bool))
        error_set(error, ERR_TYPE, s->as.if_stmt.condition->source,
                  s->as.if_stmt.condition->line,
                  s->as.if_stmt.condition->column,
                  "if condition must be Bool, found %s", type_label(condition));
      Scope then_scope = {NULL, scope}, else_scope = {NULL, scope};
      bool then_return = false, else_return = false;
      check_statements(program, &then_scope, s->as.if_stmt.then_body,
                       s->as.if_stmt.then_count, return_type, loop_depth,
                       in_function, error, &then_return);
      check_statements(program, &else_scope, s->as.if_stmt.else_body,
                       s->as.if_stmt.else_count, return_type, loop_depth,
                       in_function, error, &else_return);
      if (then_return && else_return)
        did_return = true;
      break;
    }
    case ST_WHILE: {
      Type *condition = check_expr(program, scope, s->as.while_stmt.condition,
                                   &t_bool, error);
      if (!error->set && !type_equal(condition, &t_bool))
        error_set(error, ERR_TYPE, s->as.while_stmt.condition->source,
                  s->as.while_stmt.condition->line,
                  s->as.while_stmt.condition->column,
                  "while condition must be Bool, found %s",
                  type_label(condition));
      Scope body_scope = {NULL, scope};
      bool ignored = false;
      check_statements(program, &body_scope, s->as.while_stmt.body,
                       s->as.while_stmt.count, return_type, loop_depth + 1,
                       in_function, error, &ignored);
      break;
    }
    case ST_RETURN:
      if (!in_function) {
        error_set(error, ERR_TYPE, s->source, s->line, s->column,
                  "return is only valid inside a function");
        break;
      }
      {
        Type *actual = s->as.return_value
                           ? check_expr(program, scope, s->as.return_value,
                                        return_type, error)
                           : &t_nil;
        if (!error->set && !type_equal(return_type, actual))
          error_set(error, ERR_TYPE, s->source, s->line, s->column,
                    "function must return %s, found %s",
                    type_label(return_type), type_label(actual));
        did_return = true;
      }
      break;
    case ST_BREAK:
    case ST_CONTINUE:
      if (!loop_depth)
        error_set(error, ERR_TYPE, s->source, s->line, s->column,
                  "loop control is only valid inside a while loop");
      break;
    case ST_MATCH: {
      Type *scrutinee =
          check_expr(program, scope, s->as.match_stmt.scrutinee, NULL, error);
      bool wildcard = false;
      size_t enum_count = scrutinee->kind == TYPE_ENUM
                              ? scrutinee->enumeration->variant_count
                              : 0;
      bool *seen = enum_count ? aalloc(enum_count * sizeof(bool)) : NULL;
      for (size_t a = 0; a < s->as.match_stmt.arm_count && !error->set; a++) {
        Pattern *pattern = &s->as.match_stmt.arms[a].pattern;
        if (pattern->kind == PAT_WILDCARD)
          wildcard = true;
        Scope arm_scope = {NULL, scope};
        check_pattern(scrutinee, pattern, &arm_scope, error);
        if (pattern->kind == PAT_ENUM && scrutinee->kind == TYPE_ENUM) {
          for (size_t v = 0; v < enum_count; v++)
            if (!strcmp(scrutinee->enumeration->variants[v].name,
                        pattern->variant)) {
              if (seen[v])
                error_set(error, ERR_TYPE, pattern->source, pattern->line,
                          pattern->column,
                          "duplicate match arm for enum variant '%s'",
                          pattern->variant);
              seen[v] = true;
            }
        }
        bool ignored = false;
        check_statements(program, &arm_scope, s->as.match_stmt.arms[a].body,
                         s->as.match_stmt.arms[a].body_count, return_type,
                         loop_depth, in_function, error, &ignored);
      }
      if (!error->set && !wildcard && scrutinee->kind == TYPE_ENUM) {
        for (size_t v = 0; v < enum_count; v++)
          if (!seen[v]) {
            error_set(error, ERR_TYPE, s->source, s->line, s->column,
                      "non-exhaustive match for enum '%s'; add '_'",
                      scrutinee->enumeration->name);
            break;
          }
      }
      break;
    }
    }
  }
  if (returns)
    *returns = did_return;
  return !error->set;
}
static bool check_program(Program *program, Error *error) {
  for (size_t i = 0; i < program->structure_count && !error->set; i++) {
    StructDecl *decl = program->structures[i];
    for (size_t f = 0; f < decl->field_count; f++) {
      TypeSpec *spec = (TypeSpec *)decl->fields[f].type;
      decl->fields[f].type = resolve_type(program, spec, error);
    }
  }
  for (size_t i = 0; i < program->structure_count && !error->set; i++)
    for (size_t j = i + 1; j < program->structure_count; j++)
      if (!strcmp(program->structures[i]->name, program->structures[j]->name))
        error_set(error, ERR_TYPE, program->structures[j]->source,
                  program->structures[j]->line, program->structures[j]->column,
                  "duplicate struct '%s'", program->structures[j]->name);
  for (size_t i = 0; i < program->enumeration_count && !error->set; i++)
    for (size_t j = i + 1; j < program->enumeration_count; j++)
      if (!strcmp(program->enumerations[i]->name,
                  program->enumerations[j]->name))
        error_set(error, ERR_TYPE, program->enumerations[j]->source,
                  program->enumerations[j]->line,
                  program->enumerations[j]->column, "duplicate enum '%s'",
                  program->enumerations[j]->name);
  for (size_t i = 0; i < program->function_count && !error->set; i++)
    for (size_t j = i + 1; j < program->function_count; j++)
      if (!strcmp(program->functions[i]->name, program->functions[j]->name))
        error_set(error, ERR_TYPE, program->functions[j]->source,
                  program->functions[j]->line, program->functions[j]->column,
                  "duplicate function '%s'", program->functions[j]->name);
  Scope global = {NULL, NULL};
  bool ignored = false;
  check_statements(program, &global, program->statements,
                   program->statement_count, &t_nil, 0, false, error, &ignored);
  for (size_t i = 0; i < program->function_count && !error->set; i++) {
    Function *f = program->functions[i];
    Scope local = {NULL, &global};
    for (size_t p = 0; p < f->parameter_count; p++) {
      Type *type = resolve_type(program, f->parameters[p].type, error);
      scope_define(&local, f->parameters[p].name, type, false, error,
                   f->parameters[p].source, f->parameters[p].line,
                   f->parameters[p].column);
    }
    Type *result = resolve_type(program, f->return_type, error);
    bool returned = false;
    check_statements(program, &local, f->body, f->body_count, result, 0, true,
                     error, &returned);
    if (!error->set && result->kind != TYPE_NIL && result->kind != TYPE_VOID &&
        !returned)
      error_set(error, ERR_TYPE, f->source, f->line, f->column,
                "function '%s' may finish without returning %s", f->name,
                type_label(result));
  }
  return !error->set;
}

typedef enum {
  VAL_NIL,
  VAL_INT,
  VAL_FLOAT,
  VAL_BOOL,
  VAL_STRING,
  VAL_ARRAY,
  VAL_BYTES,
  VAL_STRUCT,
  VAL_ENUM,
  VAL_OPTION,
  VAL_RESULT
} ValueKind;
typedef struct Value Value;
typedef struct {
  char *data;
  size_t length;
} StringValue;
typedef struct {
  Value *items;
  size_t length;
  Type *element;
} ArrayValue;
typedef struct {
  unsigned char *data;
  size_t length;
} BytesValue;
typedef struct {
  StructDecl *decl;
  Value *fields;
  size_t length;
} StructValue;
typedef struct {
  EnumDecl *decl;
  size_t variant;
} EnumValue;
typedef struct {
  bool present;
  Value *value;
} OptionValue;
typedef struct {
  bool ok;
  Value *value;
} ResultValue;
struct Value {
  ValueKind kind;
  union {
    int64_t integer;
    double floating;
    bool boolean;
    StringValue string;
    ArrayValue array;
    BytesValue bytes;
    StructValue structure;
    EnumValue enumeration;
    OptionValue option;
    ResultValue result;
  } as;
};
static Value val_nil(void) { return (Value){VAL_NIL, {0}}; }
static Value val_int(int64_t v) {
  Value x = {VAL_INT, {0}};
  x.as.integer = v;
  return x;
}
static Value val_float(double v) {
  Value x = {VAL_FLOAT, {0}};
  x.as.floating = v;
  return x;
}
static Value val_bool(bool v) {
  Value x = {VAL_BOOL, {0}};
  x.as.boolean = v;
  return x;
}
static Value val_string_n(const char *text, size_t length) {
  Value x = {VAL_STRING, {0}};
  x.as.string.data = astrn(text, length);
  x.as.string.length = length;
  return x;
}
static Value val_bytes_n(const unsigned char *data, size_t length) {
  Value x = {VAL_BYTES, {0}};
  x.as.bytes.data = aalloc(length ? length : 1);
  if (length)
    memcpy(x.as.bytes.data, data, length);
  x.as.bytes.length = length;
  return x;
}
static bool value_equal(Value a, Value b) {
  if (a.kind != b.kind)
    return false;
  switch (a.kind) {
  case VAL_NIL:
    return true;
  case VAL_INT:
    return a.as.integer == b.as.integer;
  case VAL_FLOAT:
    return a.as.floating == b.as.floating;
  case VAL_BOOL:
    return a.as.boolean == b.as.boolean;
  case VAL_STRING:
    return a.as.string.length == b.as.string.length &&
           !memcmp(a.as.string.data, b.as.string.data, a.as.string.length);
  case VAL_BYTES:
    return a.as.bytes.length == b.as.bytes.length &&
           !memcmp(a.as.bytes.data, b.as.bytes.data, a.as.bytes.length);
  case VAL_ARRAY:
    if (a.as.array.length != b.as.array.length)
      return false;
    for (size_t i = 0; i < a.as.array.length; i++)
      if (!value_equal(a.as.array.items[i], b.as.array.items[i]))
        return false;
    return true;
  case VAL_STRUCT:
    if (a.as.structure.decl != b.as.structure.decl)
      return false;
    for (size_t i = 0; i < a.as.structure.length; i++)
      if (!value_equal(a.as.structure.fields[i], b.as.structure.fields[i]))
        return false;
    return true;
  case VAL_ENUM:
    return a.as.enumeration.decl == b.as.enumeration.decl &&
           a.as.enumeration.variant == b.as.enumeration.variant;
  case VAL_OPTION:
    return a.as.option.present == b.as.option.present &&
           (!a.as.option.present ||
            value_equal(*a.as.option.value, *b.as.option.value));
  case VAL_RESULT:
    return a.as.result.ok == b.as.result.ok &&
           value_equal(*a.as.result.value, *b.as.result.value);
  }
  return false;
}
typedef struct {
  char *data;
  size_t length;
  size_t capacity;
} Builder;
static void builder_add(Builder *b, const char *text, size_t length) {
  size_t need;
  if (!size_add(b->length, length + 1, &need))
    fatal_oom();
  if (need > b->capacity) {
    size_t next = b->capacity ? b->capacity * 2 : 64;
    while (next < need) {
      if (next > SIZE_MAX / 2)
        fatal_oom();
      next *= 2;
    }
    char *grown = aalloc(next);
    if (b->data)
      memcpy(grown, b->data, b->length);
    b->data = grown;
    b->capacity = next;
  }
  memcpy(b->data + b->length, text, length);
  b->length += length;
  b->data[b->length] = 0;
}
static void builder_text(Builder *b, const char *text) {
  builder_add(b, text, strlen(text));
}
static void stringify(Builder *b, Value v) {
  char number[96];
  switch (v.kind) {
  case VAL_NIL:
    builder_text(b, "nil");
    break;
  case VAL_INT:
    snprintf(number, sizeof(number), "%" PRId64, v.as.integer);
    builder_text(b, number);
    break;
  case VAL_FLOAT:
    snprintf(number, sizeof(number), "%.17g", v.as.floating);
    builder_text(b, number);
    break;
  case VAL_BOOL:
    builder_text(b, v.as.boolean ? "true" : "false");
    break;
  case VAL_STRING:
    builder_add(b, v.as.string.data, v.as.string.length);
    break;
  case VAL_BYTES:
    snprintf(number, sizeof(number), "<Bytes:%zu>", v.as.bytes.length);
    builder_text(b, number);
    break;
  case VAL_ARRAY:
    builder_text(b, "[");
    for (size_t i = 0; i < v.as.array.length; i++) {
      if (i)
        builder_text(b, ", ");
      stringify(b, v.as.array.items[i]);
    }
    builder_text(b, "]");
    break;
  case VAL_STRUCT:
    builder_text(b, v.as.structure.decl->name);
    builder_text(b, "{");
    for (size_t i = 0; i < v.as.structure.length; i++) {
      if (i)
        builder_text(b, ", ");
      builder_text(b, v.as.structure.decl->fields[i].name);
      builder_text(b, ": ");
      stringify(b, v.as.structure.fields[i]);
    }
    builder_text(b, "}");
    break;
  case VAL_ENUM:
    builder_text(b, v.as.enumeration.decl->name);
    builder_text(b, "::");
    builder_text(
        b, v.as.enumeration.decl->variants[v.as.enumeration.variant].name);
    break;
  case VAL_OPTION:
    if (!v.as.option.present)
      builder_text(b, "none");
    else {
      builder_text(b, "some(");
      stringify(b, *v.as.option.value);
      builder_text(b, ")");
    }
    break;
  case VAL_RESULT:
    builder_text(b, v.as.result.ok ? "ok(" : "err(");
    stringify(b, *v.as.result.value);
    builder_text(b, ")");
    break;
  }
}
static char *value_string(Value v) {
  Builder b = {0};
  stringify(&b, v);
  return b.data ? b.data : astr("");
}
static void print_value(FILE *out, Value v) {
  char *text = value_string(v);
  fwrite(text, 1, strlen(text), out);
}
static size_t utf8_width(unsigned char c) {
  return c < 0x80                 ? 1
         : c >= 0xC2 && c <= 0xDF ? 2
         : c >= 0xE0 && c <= 0xEF ? 3
         : c >= 0xF0 && c <= 0xF4 ? 4
                                  : 0;
}
static size_t utf8_count(const char *text, size_t length) {
  size_t count = 0;
  for (size_t i = 0; i < length;) {
    size_t w = utf8_width((unsigned char)text[i]);
    if (!w)
      return 0;
    i += w;
    count++;
  }
  return count;
}

typedef struct RuntimeBinding {
  char *name;
  Value value;
  bool is_mutable;
  struct RuntimeBinding *next;
} RuntimeBinding;
typedef struct RuntimeScope {
  RuntimeBinding *bindings;
  struct RuntimeScope *parent;
} RuntimeScope;
typedef enum {
  EXEC_NORMAL,
  EXEC_RETURN,
  EXEC_BREAK,
  EXEC_CONTINUE,
  EXEC_ERROR
} ExecCode;
typedef struct {
  ExecCode code;
  Value value;
} ExecResult;
typedef struct Runtime {
  Program *program;
  RuntimeScope *global;
  Error error;
} Runtime;
static RuntimeScope *runtime_scope(RuntimeScope *parent) {
  RuntimeScope *scope = aalloc(sizeof(*scope));
  scope->parent = parent;
  return scope;
}
static RuntimeBinding *runtime_lookup(RuntimeScope *scope, const char *name) {
  for (RuntimeScope *s = scope; s; s = s->parent)
    for (RuntimeBinding *b = s->bindings; b; b = b->next)
      if (!strcmp(b->name, name))
        return b;
  return NULL;
}
static bool runtime_define(RuntimeScope *scope, const char *name, Value value,
                           bool is_mutable) {
  for (RuntimeBinding *b = scope->bindings; b; b = b->next)
    if (!strcmp(b->name, name))
      return false;
  RuntimeBinding *b = aalloc(sizeof(*b));
  b->name = astr(name);
  b->value = value;
  b->is_mutable = is_mutable;
  b->next = scope->bindings;
  scope->bindings = b;
  return true;
}
static ExecResult exec_normal(void) {
  return (ExecResult){EXEC_NORMAL, val_nil()};
}
static ExecResult exec_return(Value v) { return (ExecResult){EXEC_RETURN, v}; }
static ExecResult exec_control(ExecCode c) {
  return (ExecResult){c, val_nil()};
}
static bool checked_add_i(int64_t a, int64_t b, int64_t *out) {
  if ((b > 0 && a > INT64_MAX - b) || (b < 0 && a < INT64_MIN - b))
    return false;
  *out = a + b;
  return true;
}
static bool checked_sub_i(int64_t a, int64_t b, int64_t *out) {
  if ((b < 0 && a > INT64_MAX + b) || (b > 0 && a < INT64_MIN + b))
    return false;
  *out = a - b;
  return true;
}
static bool checked_mul_i(int64_t a, int64_t b, int64_t *out) {
  if (!a || !b) {
    *out = 0;
    return true;
  }
  if (a == -1) {
    if (b == INT64_MIN)
      return false;
    *out = -b;
    return true;
  }
  if (b == -1) {
    if (a == INT64_MIN)
      return false;
    *out = -a;
    return true;
  }
  if (a > 0) {
    if (b > 0 ? a > INT64_MAX / b : b < INT64_MIN / a)
      return false;
  } else {
    if (b > 0 ? a < INT64_MIN / b : a < INT64_MAX / b)
      return false;
  }
  *out = a * b;
  return true;
}
static bool checked_neg_i(int64_t a, int64_t *out) {
  if (a == INT64_MIN)
    return false;
  *out = -a;
  return true;
}
static bool checked_div_i(int64_t a, int64_t b, int64_t *out) {
  if (!b || (a == INT64_MIN && b == -1))
    return false;
  *out = a / b;
  return true;
}
static bool checked_rem_i(int64_t a, int64_t b, int64_t *out) {
  if (!b || (a == INT64_MIN && b == -1))
    return false;
  *out = a % b;
  return true;
}
static bool checked_abs_i(int64_t a, int64_t *out) {
  if (a == INT64_MIN)
    return false;
  *out = a < 0 ? -a : a;
  return true;
}
static bool numeric_value(Value v) {
  return v.kind == VAL_INT || v.kind == VAL_FLOAT;
}
static double numeric_float(Value v) {
  return v.kind == VAL_INT ? (double)v.as.integer : v.as.floating;
}
static Value eval_expr(Runtime *runtime, RuntimeScope *scope, Expr *expr);
static ExecResult execute_statement(Runtime *runtime, RuntimeScope *scope,
                                    Stmt *stmt);

static Value builtin_runtime(Runtime *runtime, Expr *expr, Value *args,
                             size_t count) {
  const BuiltinSpec *spec = builtin_find(expr->as.call.name);
  if (!spec)
    return val_nil();
  if ((int)count != spec->arity) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "builtin '%s' expects %d argument(s), got %zu",
              spec->name, spec->arity, count);
    return val_nil();
  }
  switch (spec->id) {
  case B_PRINT:
    print_value(stdout, args[0]);
    fflush(stdout);
    return val_nil();
  case B_PRINTLN:
    print_value(stdout, args[0]);
    fputc('\n', stdout);
    fflush(stdout);
    return val_nil();
  case B_LEN:
    if (args[0].kind == VAL_STRING) {
      if (!utf8_valid((const unsigned char *)args[0].as.string.data,
                      args[0].as.string.length)) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "len received invalid UTF-8 String");
        return val_nil();
      }
      return val_int((int64_t)utf8_count(args[0].as.string.data,
                                         args[0].as.string.length));
    }
    if (args[0].kind == VAL_ARRAY)
      return val_int((int64_t)args[0].as.array.length);
    if (args[0].kind == VAL_BYTES)
      return val_int((int64_t)args[0].as.bytes.length);
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "len expects String, Array, or Bytes");
    return val_nil();
  case B_BYTES: {
    if (args[0].kind != VAL_ARRAY) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "bytes expects Array[Int]");
      return val_nil();
    }
    unsigned char *data =
        aalloc(args[0].as.array.length ? args[0].as.array.length : 1);
    for (size_t i = 0; i < args[0].as.array.length; i++) {
      if (args[0].as.array.items[i].kind != VAL_INT ||
          args[0].as.array.items[i].as.integer < 0 ||
          args[0].as.array.items[i].as.integer > 255) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "bytes accepts only Int values in 0..255");
        return val_nil();
      }
      data[i] = (unsigned char)args[0].as.array.items[i].as.integer;
    }
    return val_bytes_n(data, args[0].as.array.length);
  }
  case B_STRING_TO_BYTES:
    if (args[0].kind != VAL_STRING ||
        !utf8_valid((const unsigned char *)args[0].as.string.data,
                    args[0].as.string.length)) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "string_to_bytes expects valid UTF-8 String");
      return val_nil();
    }
    return val_bytes_n((const unsigned char *)args[0].as.string.data,
                       args[0].as.string.length);
  case B_BYTES_TO_STRING:
    if (args[0].kind != VAL_BYTES ||
        !utf8_valid(args[0].as.bytes.data, args[0].as.bytes.length)) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "bytes_to_string expects valid UTF-8 Bytes");
      return val_nil();
    }
    return val_string_n((const char *)args[0].as.bytes.data,
                        args[0].as.bytes.length);
  case B_ARRAY_PUSH: {
    if (args[0].kind != VAL_ARRAY) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column,
                "array_push expects Array[T] as its first argument");
      return val_nil();
    }
    size_t length, bytes;
    if (!size_add(args[0].as.array.length, 1, &length) ||
        !size_mul(length, sizeof(Value), &bytes)) {
      error_set(&runtime->error, ERR_RESOURCE, expr->source, expr->line,
                expr->column, "array allocation size overflow");
      return val_nil();
    }
    Value result = {VAL_ARRAY, {0}};
    result.as.array.length = length;
    result.as.array.element = args[0].as.array.element;
    result.as.array.items = aalloc(bytes);
    memcpy(result.as.array.items, args[0].as.array.items,
           args[0].as.array.length * sizeof(Value));
    result.as.array.items[length - 1] = args[1];
    return result;
  }
  case B_INT:
    if (args[0].kind == VAL_INT)
      return args[0];
    if (args[0].kind == VAL_BOOL)
      return val_int(args[0].as.boolean ? 1 : 0);
    if (args[0].kind == VAL_FLOAT) {
      const double limit = 0x1p63;
      if (!isfinite(args[0].as.floating) || args[0].as.floating >= limit ||
          args[0].as.floating < -limit) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column,
                  "int conversion is outside the supported range");
        return val_nil();
      }
      return val_int((int64_t)args[0].as.floating);
    }
    if (args[0].kind == VAL_STRING) {
      errno = 0;
      char *end = NULL;
      intmax_t value = strtoimax(args[0].as.string.data, &end, 10);
      if (errno == ERANGE || end == args[0].as.string.data || !end || *end) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column,
                  "int conversion requires a complete decimal String");
        return val_nil();
      }
      return val_int((int64_t)value);
    }
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "int conversion does not support this value");
    return val_nil();
  case B_FLOAT:
    if (args[0].kind == VAL_FLOAT)
      return args[0];
    if (args[0].kind == VAL_INT) {
      double value = (double)args[0].as.integer;
      if (!isfinite(value)) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "float conversion is not finite");
        return val_nil();
      }
      return val_float(value);
    }
    if (args[0].kind == VAL_STRING) {
      errno = 0;
      char *end = NULL;
      double value = strtod(args[0].as.string.data, &end);
      if (errno == ERANGE || end == args[0].as.string.data || !end || *end ||
          !isfinite(value)) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column,
                  "float conversion requires a complete finite decimal String");
        return val_nil();
      }
      return val_float(value);
    }
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "float conversion accepts Int, Float, or String");
    return val_nil();
  case B_STR: {
    char *text = value_string(args[0]);
    return val_string_n(text, strlen(text));
  }
  case B_BOOL:
    if (args[0].kind == VAL_BOOL)
      return args[0];
    if (args[0].kind == VAL_NIL)
      return val_bool(false);
    if (args[0].kind == VAL_INT)
      return val_bool(args[0].as.integer != 0);
    if (args[0].kind == VAL_FLOAT)
      return val_bool(args[0].as.floating != 0.0);
    if (args[0].kind == VAL_STRING)
      return val_bool(args[0].as.string.length != 0);
    if (args[0].kind == VAL_ARRAY)
      return val_bool(args[0].as.array.length != 0);
    if (args[0].kind == VAL_BYTES)
      return val_bool(args[0].as.bytes.length != 0);
    return val_bool(true);
  case B_ASSERT:
    if (args[0].kind != VAL_BOOL || !args[0].as.boolean)
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "assertion failed");
    return val_nil();
  case B_ASSERT_EQ:
    if (!value_equal(args[0], args[1]))
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "assert_eq failed");
    return val_nil();
  case B_ABS:
    if (args[0].kind == VAL_INT) {
      int64_t result;
      if (!checked_abs_i(args[0].as.integer, &result)) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "abs cannot represent the minimum Int");
        return val_nil();
      }
      return val_int(result);
    }
    if (args[0].kind == VAL_FLOAT && isfinite(args[0].as.floating))
      return val_float(fabs(args[0].as.floating));
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "abs expects finite Int or Float");
    return val_nil();
  case B_SQRT: {
    double input = numeric_float(args[0]);
    if (!isfinite(input) || input < 0) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "sqrt expects a finite non-negative number");
      return val_nil();
    }
    double result = sqrt(input);
    if (!isfinite(result)) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "sqrt result is not finite");
      return val_nil();
    }
    return val_float(result);
  }
  case B_SOME: {
    Value *inner = aalloc(sizeof(Value));
    *inner = args[0];
    Value result = {VAL_OPTION, {0}};
    result.as.option.present = true;
    result.as.option.value = inner;
    return result;
  }
  case B_NONE: {
    Value result = {VAL_OPTION, {0}};
    return result;
  }
  case B_OK:
  case B_ERR: {
    Value *inner = aalloc(sizeof(Value));
    *inner = args[0];
    Value result = {VAL_RESULT, {0}};
    result.as.result.ok = spec->id == B_OK;
    result.as.result.value = inner;
    return result;
  }
  }
  return val_nil();
}

static Value call_runtime(Runtime *runtime, RuntimeScope *caller, Expr *expr,
                          Value *args, size_t count) {
  const BuiltinSpec *builtin = builtin_find(expr->as.call.name);
  if (builtin)
    return builtin_runtime(runtime, expr, args, count);
  Function *function = find_function(runtime->program, expr->as.call.name);
  if (!function) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "unknown function '%s'", expr->as.call.name);
    return val_nil();
  }
  RuntimeScope *local = runtime_scope(runtime->global);
  (void)caller;
  for (size_t i = 0; i < count; i++) {
    if (!runtime_define(local, function->parameters[i].name, args[i], false)) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "duplicate function parameter '%s'",
                function->parameters[i].name);
      return val_nil();
    }
  }
  for (size_t i = 0; i < function->body_count; i++) {
    ExecResult result = execute_statement(runtime, local, function->body[i]);
    if (result.code == EXEC_RETURN)
      return result.value;
    if (result.code == EXEC_BREAK || result.code == EXEC_CONTINUE) {
      error_set(&runtime->error, ERR_RUNTIME, function->body[i]->source,
                function->body[i]->line, function->body[i]->column,
                "loop control is only valid inside a while loop");
      return val_nil();
    }
    if (result.code == EXEC_ERROR || runtime->error.set)
      return val_nil();
  }
  return val_nil();
}
static Value eval_expr(Runtime *runtime, RuntimeScope *scope, Expr *expr) {
  if (runtime->error.set)
    return val_nil();
  switch (expr->kind) {
  case EX_INT:
    return val_int(expr->as.integer);
  case EX_FLOAT:
    return val_float(expr->as.floating);
  case EX_BOOL:
    return val_bool(expr->as.boolean);
  case EX_NIL:
    return val_nil();
  case EX_STRING:
    return val_string_n(expr->as.string, strlen(expr->as.string));
  case EX_VAR: {
    RuntimeBinding *binding = runtime_lookup(scope, expr->as.name);
    if (!binding) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "unknown variable '%s'", expr->as.name);
      return val_nil();
    }
    return binding->value;
  }
  case EX_ARRAY: {
    size_t bytes;
    if (!size_mul(expr->as.array.count, sizeof(Value), &bytes)) {
      error_set(&runtime->error, ERR_RESOURCE, expr->source, expr->line,
                expr->column, "array allocation size overflow");
      return val_nil();
    }
    Value result = {VAL_ARRAY, {0}};
    result.as.array.length = expr->as.array.count;
    result.as.array.items = aalloc(bytes ? bytes : 1);
    for (size_t i = 0; i < expr->as.array.count; i++) {
      result.as.array.items[i] =
          eval_expr(runtime, scope, expr->as.array.items[i]);
      if (runtime->error.set)
        return val_nil();
    }
    return result;
  }
  case EX_UNARY: {
    Value operand = eval_expr(runtime, scope, expr->as.unary.operand);
    if (runtime->error.set)
      return val_nil();
    if (expr->as.unary.op == TOK_BANG) {
      if (operand.kind != VAL_BOOL)
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "'!' expects Bool");
      return val_bool(!operand.as.boolean);
    }
    if (!numeric_value(operand)) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "unary numeric operator expects Int or Float");
      return val_nil();
    }
    if (expr->as.unary.op == TOK_PLUS)
      return operand;
    if (operand.kind == VAL_FLOAT) {
      double value = -operand.as.floating;
      if (!isfinite(value))
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "floating-point result is not finite");
      return val_float(value);
    }
    int64_t value = 0;
    if (!checked_neg_i(operand.as.integer, &value))
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "integer negation overflow");
    return runtime->error.set ? val_nil() : val_int(value);
  }
  case EX_BINARY: {
    TokenKind op = expr->as.binary.op;
    Value left = eval_expr(runtime, scope, expr->as.binary.left);
    if (runtime->error.set)
      return val_nil();
    if (op == TOK_AND_AND) {
      if (left.kind != VAL_BOOL) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "boolean operators require Bool operands");
        return val_nil();
      }
      if (!left.as.boolean)
        return val_bool(false);
      Value right = eval_expr(runtime, scope, expr->as.binary.right);
      if (runtime->error.set)
        return val_nil();
      if (right.kind != VAL_BOOL) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "boolean operators require Bool operands");
        return val_nil();
      }
      return val_bool(right.as.boolean);
    }
    if (op == TOK_OR_OR) {
      if (left.kind != VAL_BOOL) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "boolean operators require Bool operands");
        return val_nil();
      }
      if (left.as.boolean)
        return val_bool(true);
      Value right = eval_expr(runtime, scope, expr->as.binary.right);
      if (runtime->error.set)
        return val_nil();
      if (right.kind != VAL_BOOL) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "boolean operators require Bool operands");
        return val_nil();
      }
      return val_bool(right.as.boolean);
    }
    Value right = eval_expr(runtime, scope, expr->as.binary.right);
    if (runtime->error.set)
      return val_nil();
    if (op == TOK_EQUAL_EQUAL)
      return val_bool(value_equal(left, right));
    if (op == TOK_BANG_EQUAL)
      return val_bool(!value_equal(left, right));
    if (op == TOK_PLUS && left.kind == VAL_STRING && right.kind == VAL_STRING) {
      size_t length;
      if (!size_add(left.as.string.length, right.as.string.length, &length)) {
        error_set(&runtime->error, ERR_RESOURCE, expr->source, expr->line,
                  expr->column, "string allocation size overflow");
        return val_nil();
      }
      Value result = val_string_n(left.as.string.data, length);
      memcpy(result.as.string.data, left.as.string.data, left.as.string.length);
      memcpy(result.as.string.data + left.as.string.length,
             right.as.string.data, right.as.string.length);
      return result;
    }
    if (op == TOK_PLUS && left.kind == VAL_ARRAY && right.kind == VAL_ARRAY) {
      size_t length, bytes;
      if (!size_add(left.as.array.length, right.as.array.length, &length) ||
          !size_mul(length, sizeof(Value), &bytes)) {
        error_set(&runtime->error, ERR_RESOURCE, expr->source, expr->line,
                  expr->column, "array allocation size overflow");
        return val_nil();
      }
      Value result = {VAL_ARRAY, {0}};
      result.as.array.length = length;
      result.as.array.items = aalloc(bytes ? bytes : 1);
      memcpy(result.as.array.items, left.as.array.items,
             left.as.array.length * sizeof(Value));
      memcpy(result.as.array.items + left.as.array.length, right.as.array.items,
             right.as.array.length * sizeof(Value));
      return result;
    }
    if (op == TOK_PLUS && left.kind == VAL_BYTES && right.kind == VAL_BYTES) {
      size_t length;
      if (!size_add(left.as.bytes.length, right.as.bytes.length, &length)) {
        error_set(&runtime->error, ERR_RESOURCE, expr->source, expr->line,
                  expr->column, "bytes allocation size overflow");
        return val_nil();
      }
      Value result = val_bytes_n(NULL, length);
      memcpy(result.as.bytes.data, left.as.bytes.data, left.as.bytes.length);
      memcpy(result.as.bytes.data + left.as.bytes.length, right.as.bytes.data,
             right.as.bytes.length);
      return result;
    }
    if (!numeric_value(left) || !numeric_value(right) ||
        left.kind != right.kind) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "operator requires matching numeric values");
      return val_nil();
    }
    if (op == TOK_LESS || op == TOK_LESS_EQUAL || op == TOK_GREATER ||
        op == TOK_GREATER_EQUAL) {
      double a =
          left.kind == VAL_INT ? (double)left.as.integer : left.as.floating;
      double b =
          right.kind == VAL_INT ? (double)right.as.integer : right.as.floating;
      return val_bool(op == TOK_LESS         ? a < b
                      : op == TOK_LESS_EQUAL ? a <= b
                      : op == TOK_GREATER    ? a > b
                                             : a >= b);
    }
    if (left.kind == VAL_FLOAT) {
      if ((op == TOK_SLASH || op == TOK_PERCENT) && right.as.floating == 0.0) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column,
                  op == TOK_SLASH ? "division by zero" : "remainder by zero");
        return val_nil();
      }
      double value = op == TOK_PLUS    ? left.as.floating + right.as.floating
                     : op == TOK_MINUS ? left.as.floating - right.as.floating
                     : op == TOK_STAR  ? left.as.floating * right.as.floating
                                       : left.as.floating / right.as.floating;
      if (!isfinite(value)) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "floating-point result is not finite");
        return val_nil();
      }
      return val_float(value);
    }
    int64_t value = 0;
    bool ok = op == TOK_PLUS
                  ? checked_add_i(left.as.integer, right.as.integer, &value)
              : op == TOK_MINUS
                  ? checked_sub_i(left.as.integer, right.as.integer, &value)
              : op == TOK_STAR
                  ? checked_mul_i(left.as.integer, right.as.integer, &value)
              : op == TOK_SLASH
                  ? checked_div_i(left.as.integer, right.as.integer, &value)
                  : checked_rem_i(left.as.integer, right.as.integer, &value);
    if (!ok) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column,
                op == TOK_SLASH     ? "division by zero or integer overflow"
                : op == TOK_PERCENT ? "remainder by zero or integer overflow"
                                    : "checked integer arithmetic overflow");
      return val_nil();
    }
    return val_int(value);
  }
  case EX_CALL: {
    size_t bytes;
    if (!size_mul(expr->as.call.count, sizeof(Value), &bytes)) {
      error_set(&runtime->error, ERR_RESOURCE, expr->source, expr->line,
                expr->column, "call argument allocation size overflow");
      return val_nil();
    }
    Value *args = aalloc(bytes ? bytes : 1);
    for (size_t i = 0; i < expr->as.call.count; i++) {
      args[i] = eval_expr(runtime, scope, expr->as.call.args[i]);
      if (runtime->error.set)
        return val_nil();
    }
    return call_runtime(runtime, scope, expr, args, expr->as.call.count);
  }
  case EX_INDEX: {
    Value base = eval_expr(runtime, scope, expr->as.index.base);
    Value index = eval_expr(runtime, scope, expr->as.index.index);
    if (runtime->error.set)
      return val_nil();
    if (index.kind != VAL_INT || index.as.integer < 0 ||
        (uintmax_t)index.as.integer > SIZE_MAX) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column,
                "index must be a non-negative Int within the platform range");
      return val_nil();
    }
    size_t position = (size_t)index.as.integer;
    if (base.kind == VAL_ARRAY) {
      if (position >= base.as.array.length) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "array index out of bounds");
        return val_nil();
      }
      return base.as.array.items[position];
    }
    if (base.kind == VAL_BYTES) {
      if (position >= base.as.bytes.length) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "bytes index out of bounds");
        return val_nil();
      }
      return val_int(base.as.bytes.data[position]);
    }
    if (base.kind == VAL_STRING) {
      if (!utf8_valid((const unsigned char *)base.as.string.data,
                      base.as.string.length)) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "indexing received invalid UTF-8 String");
        return val_nil();
      }
      size_t offset = 0;
      for (size_t current = 0;
           current < position && offset < base.as.string.length; current++)
        offset += utf8_width((unsigned char)base.as.string.data[offset]);
      if (offset >= base.as.string.length) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "string index out of bounds");
        return val_nil();
      }
      return val_string_n(
          base.as.string.data + offset,
          utf8_width((unsigned char)base.as.string.data[offset]));
    }
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "indexing requires String, Array[T], or Bytes");
    return val_nil();
  }
  case EX_FIELD: {
    Value base = eval_expr(runtime, scope, expr->as.field.base);
    if (runtime->error.set)
      return val_nil();
    if (base.kind != VAL_STRUCT) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "field access requires a struct value");
      return val_nil();
    }
    for (size_t i = 0; i < base.as.structure.decl->field_count; i++)
      if (!strcmp(base.as.structure.decl->fields[i].name, expr->as.field.field))
        return base.as.structure.fields[i];
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "unknown struct field '%s'", expr->as.field.field);
    return val_nil();
  }
  case EX_STRUCT: {
    StructDecl *decl = find_struct(runtime->program, expr->as.structure.name);
    if (!decl) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "unknown struct '%s'", expr->as.structure.name);
      return val_nil();
    }
    Value result = {VAL_STRUCT, {0}};
    result.as.structure.decl = decl;
    result.as.structure.length = decl->field_count;
    result.as.structure.fields =
        aalloc(decl->field_count ? decl->field_count * sizeof(Value) : 1);
    for (size_t i = 0; i < expr->as.structure.count; i++) {
      for (size_t f = 0; f < decl->field_count; f++)
        if (!strcmp(decl->fields[f].name, expr->as.structure.fields[i]))
          result.as.structure.fields[f] =
              eval_expr(runtime, scope, expr->as.structure.values[i]);
    }
    return result;
  }
  case EX_ENUM: {
    EnumDecl *decl =
        find_enum(runtime->program, expr->as.enumeration.type_name);
    Value result = {VAL_ENUM, {0}};
    result.as.enumeration.decl = decl;
    for (size_t i = 0; i < decl->variant_count; i++)
      if (!strcmp(decl->variants[i].name, expr->as.enumeration.variant))
        result.as.enumeration.variant = i;
    return result;
  }
  case EX_OPTION: {
    Value result = {VAL_OPTION, {0}};
    result.as.option.present = true;
    result.as.option.value = aalloc(sizeof(Value));
    *result.as.option.value = eval_expr(runtime, scope, expr->as.option.value);
    return result;
  }
  case EX_RESULT: {
    Value result = {VAL_RESULT, {0}};
    result.as.result.ok = true;
    result.as.result.value = aalloc(sizeof(Value));
    *result.as.result.value = eval_expr(runtime, scope, expr->as.result.value);
    return result;
  }
  }
  return val_nil();
}
static bool pattern_matches(Runtime *runtime, RuntimeScope *scope,
                            Pattern *pattern, Value value) {
  if (pattern->kind == PAT_WILDCARD)
    return true;
  if (pattern->kind == PAT_NIL)
    return value.kind == VAL_NIL ||
           (value.kind == VAL_OPTION && !value.as.option.present);
  if (pattern->kind == PAT_BOOL)
    return value.kind == VAL_BOOL && value.as.boolean == pattern->boolean;
  if (pattern->kind == PAT_INT)
    return value.kind == VAL_INT && value.as.integer == pattern->integer;
  if (pattern->kind == PAT_STRING)
    return value.kind == VAL_STRING &&
           !strcmp(value.as.string.data, pattern->text);
  if (pattern->kind == PAT_ENUM)
    return value.kind == VAL_ENUM &&
           (!pattern->type_name ||
            !strcmp(pattern->type_name, value.as.enumeration.decl->name)) &&
           !strcmp(
               value.as.enumeration.decl->variants[value.as.enumeration.variant]
                   .name,
               pattern->variant);
  if (pattern->kind == PAT_OPTION) {
    if (value.kind != VAL_OPTION || value.as.option.present != pattern->ok)
      return false;
    if (pattern->binding && value.as.option.present)
      runtime_define(scope, pattern->binding, *value.as.option.value, false);
    return true;
  }
  if (pattern->kind == PAT_RESULT) {
    if (value.kind != VAL_RESULT || value.as.result.ok != pattern->ok)
      return false;
    if (pattern->binding)
      runtime_define(scope, pattern->binding, *value.as.result.value, false);
    return true;
  }
  runtime->error.set = true;
  return false;
}
static ExecResult execute_statement(Runtime *runtime, RuntimeScope *scope,
                                    Stmt *stmt) {
  if (runtime->error.set)
    return exec_control(EXEC_ERROR);
  switch (stmt->kind) {
  case ST_LET: {
    Value value = eval_expr(runtime, scope, stmt->as.let.initializer);
    if (runtime->error.set)
      return exec_control(EXEC_ERROR);
    if (!runtime_define(scope, stmt->as.let.name, value,
                        stmt->as.let.is_mutable))
      error_set(&runtime->error, ERR_RUNTIME, stmt->source, stmt->line,
                stmt->column, "binding '%s' is already defined in this scope",
                stmt->as.let.name);
    return exec_normal();
  }
  case ST_EXPR:
    (void)eval_expr(runtime, scope, stmt->as.expression);
    return runtime->error.set ? exec_control(EXEC_ERROR) : exec_normal();
  case ST_ASSIGN: {
    if (stmt->as.assign.target->kind != EX_VAR) {
      error_set(&runtime->error, ERR_RUNTIME, stmt->source, stmt->line,
                stmt->column, "only variable bindings can be assigned");
      return exec_control(EXEC_ERROR);
    }
    Value value = eval_expr(runtime, scope, stmt->as.assign.value);
    if (runtime->error.set)
      return exec_control(EXEC_ERROR);
    RuntimeBinding *binding =
        runtime_lookup(scope, stmt->as.assign.target->as.name);
    if (!binding)
      error_set(&runtime->error, ERR_RUNTIME, stmt->source, stmt->line,
                stmt->column, "unknown variable '%s'",
                stmt->as.assign.target->as.name);
    else if (!binding->is_mutable)
      error_set(&runtime->error, ERR_RUNTIME, stmt->source, stmt->line,
                stmt->column, "cannot assign to immutable binding '%s'",
                stmt->as.assign.target->as.name);
    else
      binding->value = value;
    return runtime->error.set ? exec_control(EXEC_ERROR) : exec_normal();
  }
  case ST_RETURN:
    return exec_return(stmt->as.return_value
                           ? eval_expr(runtime, scope, stmt->as.return_value)
                           : val_nil());
  case ST_BREAK:
    return exec_control(EXEC_BREAK);
  case ST_CONTINUE:
    return exec_control(EXEC_CONTINUE);
  case ST_IF: {
    Value condition = eval_expr(runtime, scope, stmt->as.if_stmt.condition);
    if (runtime->error.set)
      return exec_control(EXEC_ERROR);
    if (condition.kind != VAL_BOOL) {
      error_set(&runtime->error, ERR_RUNTIME, stmt->source, stmt->line,
                stmt->column, "if condition must be Bool");
      return exec_control(EXEC_ERROR);
    }
    Stmt **body = condition.as.boolean ? stmt->as.if_stmt.then_body
                                       : stmt->as.if_stmt.else_body;
    size_t count = condition.as.boolean ? stmt->as.if_stmt.then_count
                                        : stmt->as.if_stmt.else_count;
    RuntimeScope *branch = runtime_scope(scope);
    for (size_t i = 0; i < count; i++) {
      ExecResult result = execute_statement(runtime, branch, body[i]);
      if (result.code != EXEC_NORMAL)
        return result;
    }
    return exec_normal();
  }
  case ST_WHILE: {
    for (;;) {
      Value condition =
          eval_expr(runtime, scope, stmt->as.while_stmt.condition);
      if (runtime->error.set)
        return exec_control(EXEC_ERROR);
      if (condition.kind != VAL_BOOL) {
        error_set(&runtime->error, ERR_RUNTIME, stmt->source, stmt->line,
                  stmt->column, "while condition must be Bool");
        return exec_control(EXEC_ERROR);
      }
      if (!condition.as.boolean)
        break;
      RuntimeScope *loop = runtime_scope(scope);
      bool continued = false;
      for (size_t i = 0; i < stmt->as.while_stmt.count; i++) {
        ExecResult result =
            execute_statement(runtime, loop, stmt->as.while_stmt.body[i]);
        if (result.code == EXEC_BREAK)
          return exec_normal();
        if (result.code == EXEC_CONTINUE) {
          continued = true;
          break;
        }
        if (result.code != EXEC_NORMAL)
          return result;
      }
      if (continued)
        continue;
    }
    return exec_normal();
  }
  case ST_MATCH: {
    Value value = eval_expr(runtime, scope, stmt->as.match_stmt.scrutinee);
    if (runtime->error.set)
      return exec_control(EXEC_ERROR);
    for (size_t a = 0; a < stmt->as.match_stmt.arm_count; a++) {
      RuntimeScope *arm = runtime_scope(scope);
      if (pattern_matches(runtime, arm, &stmt->as.match_stmt.arms[a].pattern,
                          value)) {
        for (size_t i = 0; i < stmt->as.match_stmt.arms[a].body_count; i++) {
          ExecResult result = execute_statement(
              runtime, arm, stmt->as.match_stmt.arms[a].body[i]);
          if (result.code != EXEC_NORMAL)
            return result;
        }
        return exec_normal();
      }
    }
    error_set(&runtime->error, ERR_RUNTIME, stmt->source, stmt->line,
              stmt->column, "match expression had no matching arm");
    return exec_control(EXEC_ERROR);
  }
  }
  return exec_normal();
}
static int run_program(Program *program) {
  Runtime runtime = {program, NULL, {0}};
  runtime.global = runtime_scope(NULL);
  for (size_t i = 0; i < program->statement_count; i++) {
    ExecResult result =
        execute_statement(&runtime, runtime.global, program->statements[i]);
    if (result.code == EXEC_RETURN)
      error_set(&runtime.error, ERR_RUNTIME, program->statements[i]->source,
                program->statements[i]->line, program->statements[i]->column,
                "return is only valid inside a function");
    else if (result.code == EXEC_BREAK || result.code == EXEC_CONTINUE)
      error_set(&runtime.error, ERR_RUNTIME, program->statements[i]->source,
                program->statements[i]->line, program->statements[i]->column,
                "loop control is only valid inside a while loop");
    if (runtime.error.set)
      break;
  }
  if (runtime.error.set) {
    print_error(&runtime.error);
    return 1;
  }
  return 0;
}

static char *read_file(const char *path, size_t *length, Error *error) {
  FILE *file = fopen(path, "rb");
  if (!file) {
    error_set(error, ERR_IO, NULL, 1, 1, "cannot open '%s': %s", path,
              strerror(errno));
    return NULL;
  }
  if (fseek(file, 0, SEEK_END) != 0) {
    fclose(file);
    error_set(error, ERR_IO, NULL, 1, 1, "cannot seek '%s'", path);
    return NULL;
  }
  long end = ftell(file);
  if (end < 0) {
    fclose(file);
    error_set(error, ERR_IO, NULL, 1, 1, "cannot determine the size of '%s'",
              path);
    return NULL;
  }
  if (fseek(file, 0, SEEK_SET) != 0) {
    fclose(file);
    error_set(error, ERR_IO, NULL, 1, 1, "cannot rewind '%s'", path);
    return NULL;
  }
  size_t size = (size_t)end;
  char *data = malloc(size + 1);
  if (!data) {
    fclose(file);
    error_set(error, ERR_RESOURCE, NULL, 1, 1, "file '%s' is too large to read",
              path);
    return NULL;
  }
  size_t got = fread(data, 1, size, file);
  int close_result = fclose(file);
  if (got != size || close_result != 0) {
    free(data);
    error_set(error, ERR_IO, NULL, 1, 1, "cannot read '%s'", path);
    return NULL;
  }
  data[size] = 0;
  *length = size;
  return data;
}
static const char artifact_magic[] = "KRYNATIVE1\n";
static bool is_artifact(const unsigned char *data, size_t length) {
  size_t magic_length = sizeof(artifact_magic) - 1;
  if (length >= magic_length && !memcmp(data, artifact_magic, magic_length))
    return true;
  return length == magic_length - 1 && !memcmp(data, artifact_magic, length);
}
static char *artifact_payload(const unsigned char *data, size_t length,
                              size_t *payload_length, Error *error,
                              const char *path) {
  size_t header = sizeof(artifact_magic) - 1;
  if (length < header + 8) {
    error_set(error, ERR_ARTIFACT, NULL, 1, 1,
              "%s: malformed native artifact: truncated header", path);
    return NULL;
  }
  uint64_t encoded = 0;
  for (size_t i = 0; i < 8; i++)
    encoded |= (uint64_t)data[header + i] << (8 * i);
  if (encoded > SIZE_MAX || encoded != length - header - 8) {
    error_set(error, ERR_ARTIFACT, NULL, 1, 1,
              "%s: malformed native artifact: payload length does not match "
              "file length",
              path);
    return NULL;
  }
  *payload_length = (size_t)encoded;
  char *payload = malloc(*payload_length + 1);
  if (!payload) {
    error_set(error, ERR_RESOURCE, NULL, 1, 1, "artifact payload is too large");
    return NULL;
  }
  memcpy(payload, data + header + 8, *payload_length);
  payload[*payload_length] = 0;
  return payload;
}
static bool write_artifact(const char *path, const char *source, size_t length,
                           Error *error) {
  if ((uintmax_t)length > UINT64_MAX) {
    error_set(error, ERR_ARTIFACT, NULL, 1, 1,
              "source is too large for the native artifact format");
    return false;
  }
  FILE *file = fopen(path, "wb");
  if (!file) {
    error_set(error, ERR_IO, NULL, 1, 1, "cannot create '%s': %s", path,
              strerror(errno));
    return false;
  }
  bool ok = fwrite(artifact_magic, 1, sizeof(artifact_magic) - 1, file) ==
            sizeof(artifact_magic) - 1;
  uint64_t size = (uint64_t)length;
  for (int i = 0; i < 8 && ok; i++) {
    unsigned char byte = (unsigned char)((size >> (8 * i)) & 255);
    ok = fwrite(&byte, 1, 1, file) == 1;
  }
  if (ok)
    ok = fwrite(source, 1, length, file) == length;
  if (fclose(file) != 0)
    ok = false;
  if (!ok)
    error_set(error, ERR_IO, NULL, 1, 1, "failed while writing '%s'", path);
  return ok;
}

typedef struct ModuleLoader {
  char **paths;
  size_t count;
  size_t capacity;
  char **stack;
  size_t stack_count;
  size_t stack_capacity;
  char *root_dir;
  Error *error;
} ModuleLoader;
static bool path_has_parent(const char *path) {
  const char *part = path;
  while (*part) {
    while (*part == '/')
      part++;
    const char *end = strchr(part, '/');
    size_t length = end ? (size_t)(end - part) : strlen(part);
    if (length == 2 && !strncmp(part, "..", 2))
      return true;
    if (!end)
      break;
    part = end + 1;
  }
  return false;
}
static char *path_join(const char *directory, const char *relative) {
  size_t a = strlen(directory), b = strlen(relative), total;
  if (!size_add(a, b + 2, &total))
    return NULL;
  char *result = aalloc(total);
  snprintf(result, total, "%s/%s", directory, relative);
  return result;
}
static bool path_prefix(const char *path, const char *root) {
  size_t length = strlen(root);
  return !strncmp(path, root, length) &&
         (path[length] == '/' || path[length] == 0);
}
static bool loader_seen(ModuleLoader *loader, const char *path) {
  for (size_t i = 0; i < loader->count; i++)
    if (!strcmp(loader->paths[i], path))
      return true;
  return false;
}
static void loader_add(ModuleLoader *loader, const char *path) {
  loader->paths = agrow(loader->paths, loader->count, &loader->capacity,
                        loader->count + 1, sizeof(char *));
  loader->paths[loader->count++] = astr(path);
}
static bool loader_on_stack(ModuleLoader *loader, const char *path) {
  for (size_t i = 0; i < loader->stack_count; i++)
    if (!strcmp(loader->stack[i], path))
      return true;
  return false;
}
static Program *load_module(ModuleLoader *loader, const char *path,
                            Program *target);
/* A separate append helper is used for module arrays because their capacity is
 * local. */
static void merge_public_declarations(Program *target, Program *module,
                                      ModuleLoader *loader) {
  size_t fc = target->function_count, sc = target->structure_count,
         ec = target->enumeration_count;
  for (size_t i = 0; i < module->function_count && !loader->error->set; i++)
    if (module->functions[i]->is_public) {
      for (size_t j = 0; j < target->function_count; j++)
        if (!strcmp(target->functions[j]->name, module->functions[i]->name)) {
          error_set(loader->error, ERR_TYPE, module->functions[i]->source,
                    module->functions[i]->line, module->functions[i]->column,
                    "duplicate imported function '%s'",
                    module->functions[i]->name);
          break;
        }
      target->functions = agrow(target->functions, target->function_count, &fc,
                                target->function_count + 1, sizeof(Function *));
      target->functions[target->function_count++] = module->functions[i];
    }
  for (size_t i = 0; i < module->structure_count && !loader->error->set; i++)
    if (module->structures[i]->is_public) {
      for (size_t j = 0; j < target->structure_count; j++)
        if (!strcmp(target->structures[j]->name, module->structures[i]->name)) {
          error_set(loader->error, ERR_TYPE, module->structures[i]->source,
                    module->structures[i]->line, module->structures[i]->column,
                    "duplicate imported struct '%s'",
                    module->structures[i]->name);
          break;
        }
      target->structures =
          agrow(target->structures, target->structure_count, &sc,
                target->structure_count + 1, sizeof(StructDecl *));
      target->structures[target->structure_count++] = module->structures[i];
    }
  for (size_t i = 0; i < module->enumeration_count && !loader->error->set; i++)
    if (module->enumerations[i]->is_public) {
      for (size_t j = 0; j < target->enumeration_count; j++)
        if (!strcmp(target->enumerations[j]->name,
                    module->enumerations[i]->name)) {
          error_set(
              loader->error, ERR_TYPE, module->enumerations[i]->source,
              module->enumerations[i]->line, module->enumerations[i]->column,
              "duplicate imported enum '%s'", module->enumerations[i]->name);
          break;
        }
      target->enumerations =
          agrow(target->enumerations, target->enumeration_count, &ec,
                target->enumeration_count + 1, sizeof(EnumDecl *));
      target->enumerations[target->enumeration_count++] =
          module->enumerations[i];
    }
}
static Program *load_module(ModuleLoader *loader, const char *path,
                            Program *target) {
  char resolved[PATH_MAX];
  if (!realpath(path, resolved)) {
    error_set(loader->error, ERR_IO, NULL, 1, 1,
              "cannot resolve module '%s': %s", path, strerror(errno));
    return NULL;
  }
  if (!path_prefix(resolved, loader->root_dir)) {
    error_set(loader->error, ERR_IO, NULL, 1, 1,
              "module path escapes the project root: '%s'", path);
    return NULL;
  }
  if (loader_on_stack(loader, resolved)) {
    error_set(loader->error, ERR_TYPE, NULL, 1, 1,
              "cyclic module import involving '%s'", resolved);
    return NULL;
  }
  if (loader_seen(loader, resolved))
    return target;
  loader_add(loader, resolved);
  loader->stack =
      agrow(loader->stack, loader->stack_count, &loader->stack_capacity,
            loader->stack_count + 1, sizeof(char *));
  loader->stack[loader->stack_count++] = astr(resolved);
  Error read_error = {0};
  size_t length = 0;
  char *data = read_file(resolved, &length, &read_error);
  if (!data) {
    *loader->error = read_error;
    return NULL;
  }
  if (is_artifact((unsigned char *)data, length)) {
    free(data);
    error_set(loader->error, ERR_ARTIFACT, NULL, 1, 1,
              "modules must import source files, not native artifacts");
    return NULL;
  }
  Source *source = source_make(resolved, data, length);
  free(data);
  Error parse_error = {0};
  Program *module = parse_program(source, &parse_error);
  if (!module) {
    *loader->error = parse_error;
    return NULL;
  }
  for (size_t i = 0; i < module->import_count && !loader->error->set; i++) {
    char *relative = module->imports[i].path;
    if (relative[0] == '/' || path_has_parent(relative)) {
      error_set(loader->error, ERR_IO, module->imports[i].source,
                module->imports[i].line, module->imports[i].column,
                "unsafe module path '%s'", relative);
      break;
    }
    char *with_extension = astr(relative);
    if (!strstr(relative, ".kry")) {
      size_t n = strlen(relative);
      with_extension = aalloc(n + 5);
      snprintf(with_extension, n + 5, "%s.kry", relative);
    }
    char module_dir[PATH_MAX];
    snprintf(module_dir, sizeof(module_dir), "%s", resolved);
    char *slash = strrchr(module_dir, '/');
    if (slash)
      *slash = 0;
    char *joined = path_join(module_dir, with_extension);
    load_module(loader, joined, target);
    if (loader->error->set)
      break;
  }
  if (!loader->error->set)
    merge_public_declarations(target, module, loader);
  loader->stack_count--;
  return loader->error->set ? NULL : target;
}
static Program *load_root(const char *path, const char *source_text,
                          size_t source_length, Error *error) {
  Source *root_source = source_make(path, source_text, source_length);
  Program *program = parse_program(root_source, error);
  if (!program)
    return NULL;
  char resolved[PATH_MAX];
  if (!realpath(path, resolved)) {
    if (path[0] == '/' || strchr(path, '/'))
      snprintf(resolved, sizeof(resolved), "%s", path);
    else {
      if (!getcwd(resolved, sizeof(resolved)))
        snprintf(resolved, sizeof(resolved), ".");
      size_t n = strlen(resolved);
      if (n + 1 < sizeof(resolved)) {
        resolved[n] = '/';
        resolved[n + 1] = 0;
        strncat(resolved, path, sizeof(resolved) - strlen(resolved) - 1);
      }
    }
  }
  ModuleLoader loader = {0};
  loader.error = error;
  loader.root_dir = astr(resolved);
  char *slash = strrchr(loader.root_dir, '/');
  if (slash)
    *slash = 0;
  for (size_t i = 0; i < program->import_count && !error->set; i++) {
    char *relative = program->imports[i].path;
    if (relative[0] == '/' || path_has_parent(relative)) {
      error_set(error, ERR_IO, program->imports[i].source,
                program->imports[i].line, program->imports[i].column,
                "unsafe module path '%s'", relative);
      break;
    }
    char *with_extension = astr(relative);
    if (!strstr(relative, ".kry")) {
      size_t n = strlen(relative);
      with_extension = aalloc(n + 5);
      snprintf(with_extension, n + 5, "%s.kry", relative);
    }
    char *joined = path_join(loader.root_dir, with_extension);
    load_module(&loader, joined, program);
  }
  return error->set ? NULL : program;
}

static int process_source(const char *path, const char *data, size_t length,
                          bool check_only) {
  Arena arena = {0};
  Arena *previous = arena_current;
  arena_current = &arena;
  Error error = {0};
  Program *program = load_root(path, data, length, &error);
  int result = 1;
  if (program && check_program(program, &error)) {
    if (!check_only)
      result = run_program(program);
    else
      result = 0;
  }
  if (error.set)
    print_error(&error);
  arena_free(&arena);
  arena_current = previous;
  return result;
}
static char *default_output(const char *input) {
  const char *dot = strrchr(input, '.');
  size_t length = dot ? (size_t)(dot - input) : strlen(input);
  char *output = malloc(length + 6);
  if (!output)
    return NULL;
  memcpy(output, input, length);
  memcpy(output + length, ".kexe", 6);
  return output;
}
static char *format_text(const char *source, size_t length,
                         size_t *output_length) {
  char *output = malloc(length + 2);
  if (!output)
    return NULL;
  size_t used = 0;
  size_t line_start = 0;
  for (size_t i = 0; i <= length; i++) {
    if (i == length || source[i] == '\n') {
      size_t end = i;
      while (end > line_start &&
             (source[end - 1] == ' ' || source[end - 1] == '\t' ||
              source[end - 1] == '\r'))
        end--;
      memcpy(output + used, source + line_start, end - line_start);
      used += end - line_start;
      if (i < length)
        output[used++] = '\n';
      line_start = i + 1;
    }
  }
  if (used == 0 || output[used - 1] != '\n')
    output[used++] = '\n';
  output[used] = 0;
  *output_length = used;
  return output;
}
static int process_repl_line(const char *data, size_t length);
static int run_repl(void) {
  char line[4096];
  bool interactive = isatty(STDIN_FILENO);
  if (interactive)
    puts("Kryndel REPL 1.1.0. Enter :help for help or :quit to exit.");
  for (;;) {
    if (interactive) {
      fputs("kry> ", stdout);
      fflush(stdout);
    }
    if (!fgets(line, sizeof(line), stdin))
      break;
    if (!strcmp(line, ":quit\n") || !strcmp(line, ":q\n"))
      break;
    if (!strcmp(line, ":help\n")) {
      puts(":help  Show this help\n:quit  Exit the REPL");
      continue;
    }
    size_t length = strlen(line);
    if (length && line[length - 1] == '\n')
      line[--length] = 0;
    if (!length)
      continue;
    int result = process_repl_line(line, length);
    if (result != 0 && interactive) {
    }
  }
  return 0;
}
static bool compiler_available(const char *candidate) {
  if (!candidate || !*candidate)
    return false;
  if (strchr(candidate, '/'))
    return access(candidate, X_OK) == 0;
  const char *path = getenv("PATH");
  if (!path || !*path)
    path = "/usr/bin:/bin";
  size_t name_length = strlen(candidate);
  const char *start = path;
  while (*start) {
    const char *end = strchr(start, ':');
    size_t directory_length = end ? (size_t)(end - start) : strlen(start);
    size_t total;
    if (!size_add(directory_length, name_length + 2, &total))
      return false;
    char *full = malloc(total);
    if (!full)
      return false;
    if (directory_length) {
      memcpy(full, start, directory_length);
      full[directory_length] = '/';
      memcpy(full + directory_length + 1, candidate, name_length + 1);
    } else {
      memcpy(full, candidate, name_length + 1);
    }
    bool available = access(full, X_OK) == 0;
    free(full);
    if (available)
      return true;
    if (!end)
      break;
    start = end + 1;
  }
  return false;
}
static int run_doctor(void) {
  bool source_ok = access("native/kry.c", R_OK) == 0;
  bool build_ok = access(".", W_OK) == 0;
  const char *configured_cc = getenv("CC");
  bool compiler_ok = configured_cc && *configured_cc
                         ? compiler_available(configured_cc)
                         : compiler_available("cc") || compiler_available("gcc") ||
                               compiler_available("clang");
  printf("Kryndel doctor\n");
  printf("native compiler: %s\n", compiler_ok ? "available" : "unavailable");
  printf("native source: %s\n", source_ok ? "available" : "missing");
  printf("output directory: %s\n", build_ok ? "writable" : "not writable");
  printf("environment: %s\n",
         getenv("LC_ALL") || getenv("LANG") ? "configured" : "default locale");
  if (!compiler_ok || !source_ok || !build_ok) {
    puts("doctor: issues found");
    return 1;
  }
  puts("doctor: ready");
  return 0;
}
static void usage(void) {
  puts("Kryndel 1.1.0 — strict native language toolchain");
  puts("Usage:");
  puts("  kry check <file.kry>              Parse and statically validate "
       "source");
  puts("  kry run <file.kry|file.kexe>     Check and execute source or "
       "artifact");
  puts("  kry build <file.kry> [-o FILE]   Check and write deterministic "
       "artifact");
  puts("  kry fmt [--check|-w] <file.kry>  Format valid source "
       "deterministically");
  puts("  kry repl                          Start the interactive REPL");
  puts("  kry doctor                        Check the native installation");
  puts("  kry version                      Print the compiler version");
  puts("  kry --help                       Show this help");
  puts("\nSource syntax uses let/let mut, typed functions, Bool conditions, "
       "Array[T], modules, structs, enums, and match.");
}
static int cli_error(const char *message) {
  fprintf(stderr, "error[cli]: %s\n", message);
  return 2;
}
int main(int argc, char **argv) {
  if (argc < 2) {
    usage();
    return 2;
  }
  if (!strcmp(argv[1], "--help") || !strcmp(argv[1], "-h")) {
    usage();
    return 0;
  }
  if (!strcmp(argv[1], "version") || !strcmp(argv[1], "--version")) {
    puts("Kryndel 1.1.0");
    return 0;
  }
  if (!strcmp(argv[1], "repl")) {
    if (argc != 2)
      return cli_error("repl does not accept positional arguments");
    return run_repl();
  }
  if (!strcmp(argv[1], "doctor")) {
    if (argc != 2)
      return cli_error("doctor does not accept positional arguments");
    return run_doctor();
  }
  if (!strcmp(argv[1], "fmt")) {
    bool check_mode = false, write_mode = false;
    const char *path = NULL;
    for (int i = 2; i < argc; i++) {
      if (!strcmp(argv[i], "--check"))
        check_mode = true;
      else if (!strcmp(argv[i], "-w") || !strcmp(argv[i], "--write"))
        write_mode = true;
      else if (path)
        return cli_error("fmt accepts exactly one source path");
      else
        path = argv[i];
    }
    if (!path)
      return cli_error("fmt requires a source path");
    Error read_error = {0};
    size_t length = 0;
    char *data = read_file(path, &length, &read_error);
    if (!data) {
      print_error(&read_error);
      return 1;
    }
    int valid = process_source(path, data, length, true);
    if (valid) {
      free(data);
      return valid;
    }
    size_t formatted_length = 0;
    char *formatted = format_text(data, length, &formatted_length);
    if (!formatted) {
      free(data);
      return 2;
    }
    int result = 0;
    if (check_mode &&
        (formatted_length != length || memcmp(formatted, data, length))) {
      fprintf(stderr, "error[cli]: %s would be reformatted\n", path);
      result = 1;
    } else if (write_mode) {
      FILE *file = fopen(path, "wb");
      bool write_ok = false;
      if (file) {
        write_ok = fwrite(formatted, 1, formatted_length, file) == formatted_length;
        if (fclose(file) != 0)
          write_ok = false;
      }
      if (!write_ok) {
        fprintf(stderr, "error[io]: cannot write '%s'\n", path);
        result = 1;
      }
    } else if (!check_mode)
      fwrite(formatted, 1, formatted_length, stdout);
    free(formatted);
    free(data);
    return result;
  }
  bool check_only = false, build = false;
  const char *input = NULL, *output = NULL;
  int index = 1;
  if (!strcmp(argv[index], "check")) {
    check_only = true;
    if (argc != 3)
      return cli_error("check requires exactly one source path");
    input = argv[2];
  } else if (!strcmp(argv[index], "run")) {
    if (argc != 3)
      return cli_error("run requires exactly one path");
    input = argv[2];
  } else if (!strcmp(argv[index], "build")) {
    build = true;
    if (argc != 3 && argc != 5)
      return cli_error("build expects a source path and optional -o FILE");
    input = argv[2];
    if (argc == 5) {
      if (strcmp(argv[3], "-o"))
        return cli_error("build expects -o before its output path");
      output = argv[4];
    }
  } else {
    if (argc != 2)
      return cli_error("a source shorthand accepts exactly one path");
    input = argv[1];
  }
  Error read_error = {0};
  size_t length = 0;
  char *data = read_file(input, &length, &read_error);
  if (!data) {
    print_error(&read_error);
    return 1;
  }
  if (build) {
    if (is_artifact((unsigned char *)data, length)) {
      fprintf(stderr,
              "error[artifact]: build expects source, not an artifact\n");
      free(data);
      return 2;
    }
    int valid = process_source(input, data, length, true);
    if (valid) {
      free(data);
      return valid;
    }
    char *default_path = NULL;
    if (!output) {
      default_path = default_output(input);
      output = default_path;
    }
    Error write_error = {0};
    bool ok = write_artifact(output, data, length, &write_error);
    if (ok)
      printf("built %s\n", output);
    else
      print_error(&write_error);
    free(default_path);
    free(data);
    return ok ? 0 : 1;
  }
  if (is_artifact((unsigned char *)data, length)) {
    size_t payload_length = 0;
    Error artifact_error = {0};
    char *payload = artifact_payload((unsigned char *)data, length,
                                     &payload_length, &artifact_error, input);
    free(data);
    if (!payload) {
      print_error(&artifact_error);
      return 1;
    }
    int result = process_source(input, payload, payload_length, check_only);
    free(payload);
    return result;
  }
  int result = process_source(input, data, length, check_only);
  free(data);
  return result;
}

static int process_repl_line(const char *data, size_t length) {
  Arena arena = {0};
  Arena *previous = arena_current;
  arena_current = &arena;
  Error error = {0};
  Program *program = load_root("<repl>", data, length, &error);
  int result = 1;
  if (program && check_program(program, &error)) {
    if (program->statement_count == 1 && program->statements[0]->kind == ST_EXPR) {
      Runtime runtime = {program, NULL, {0}};
      runtime.global = runtime_scope(NULL);
      Value value = eval_expr(&runtime, runtime.global,
                              program->statements[0]->as.expression);
      if (runtime.error.set)
        error = runtime.error;
      else {
        print_value(stdout, value);
        fputc('\n', stdout);
        result = 0;
      }
    } else {
      result = run_program(program);
    }
  }
  if (error.set)
    print_error(&error);
  arena_free(&arena);
  arena_current = previous;
  return result;
}
