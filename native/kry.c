#define _POSIX_C_SOURCE 200809L
#define _XOPEN_SOURCE 700
#define KRY_VERSION "1.2.0"

#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <limits.h>
#include <locale.h>
#include <math.h>
#include <pthread.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

/* One native implementation, one checked AST, and one runtime semantic path. */

typedef struct Arena {
  void **items;
  size_t count;
  size_t capacity;
} Arena;
static _Thread_local Arena *arena_current;
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
    size_t next = 64;
    if (arena_current->capacity) {
      if (arena_current->capacity > SIZE_MAX / 2)
        fatal_oom();
      next = arena_current->capacity * 2;
    }
    size_t bytes = 0;
    if (!size_mul(next, sizeof(void *), &bytes))
      fatal_oom();
    void **grown = realloc(arena_current->items, bytes);
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
  size_t next = 8;
  if (*capacity) {
    if (*capacity > SIZE_MAX / 2)
      fatal_oom();
    next = *capacity * 2;
  }
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
  if (length)
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
static bool json_diagnostics;
static bool restricted_mode;
static char *sandbox_root;
static size_t max_source_bytes = 16U * 1024U * 1024U;
static size_t max_artifact_bytes = 64U * 1024U * 1024U;
static const char *error_code(ErrorKind kind) {
  switch (kind) {
  case ERR_LEX: return "KRY001";
  case ERR_PARSE: return "KRY002";
  case ERR_TYPE: return "KRY003";
  case ERR_RUNTIME: return "KRY004";
  case ERR_ARTIFACT: return "KRY005";
  case ERR_CLI: return "KRY006";
  case ERR_IO: return "KRY007";
  case ERR_RESOURCE: return "KRY008";
  }
  return "KRY000";
}
static void json_string(FILE *out, const char *text) {
  fputc('"', out);
  for (const unsigned char *p = (const unsigned char *)text; *p; p++) {
    if (*p == '"' || *p == '\\') { fputc('\\', out); fputc(*p, out); }
    else if (*p == '\n') fputs("\\n", out);
    else if (*p == '\r') fputs("\\r", out);
    else if (*p == '\t') fputs("\\t", out);
    else if (*p < 0x20) fprintf(out, "\\u%04x", *p);
    else fputc(*p, out);
  }
  fputc('"', out);
}
static void print_error(const Error *error) {
  const char *name = error->source ? error->source->name : "<input>";
  if (json_diagnostics) {
    printf("{\"code\":\"");
    fputs(error_code(error->kind), stdout);
    printf("\",\"category\":"); json_string(stdout, error_name(error->kind));
    printf(",\"severity\":\"error\",\"source\":"); json_string(stdout, name);
    printf(",\"line\":%d,\"column\":%d,\"message\":", error->line, error->column);
    json_string(stdout, error->message);
    printf("}\n");
    return;
  }
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
  TYPE_UNKNOWN,
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
  TYPE_CHANNEL,
  TYPE_THREAD,
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
static Type t_unknown = {TYPE_UNKNOWN, "<unknown>", NULL, NULL, NULL, NULL};
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
  return type_new(TYPE_ARRAY, "Array", first ? first : &t_unknown, NULL);
}
static Type *type_option(Type *first) {
  return type_new(TYPE_OPTION, "Option", first ? first : &t_unknown, NULL);
}
static Type *type_result(Type *first, Type *second) {
  return type_new(TYPE_RESULT, "Result", first ? first : &t_unknown,
                  second ? second : &t_unknown);
}
static Type *type_channel(Type *first) {
  return type_new(TYPE_CHANNEL, "Channel", first ? first : &t_unknown, NULL);
}
static Type *type_thread(Type *first) {
  return type_new(TYPE_THREAD, "Thread", first ? first : &t_unknown, NULL);
}
static bool struct_type_copyable(StructDecl *structure);
static bool type_equal(Type *a, Type *b) {
  if (!a || !b || a->kind == TYPE_UNKNOWN || b->kind == TYPE_UNKNOWN ||
      a->kind != b->kind)
    return false;
  if (a->kind == TYPE_ARRAY)
    return type_equal(a->first, b->first);
  if (a->kind == TYPE_OPTION)
    return type_equal(a->first, b->first);
  if (a->kind == TYPE_RESULT)
    return type_equal(a->first, b->first) && type_equal(a->second, b->second);
  if (a->kind == TYPE_CHANNEL || a->kind == TYPE_THREAD)
    return type_equal(a->first, b->first);
  if (a->kind == TYPE_STRUCT)
    return a->structure == b->structure;
  if (a->kind == TYPE_ENUM)
    return a->enumeration == b->enumeration;
  return true;
}
static bool type_numeric(Type *type) {
  return type && (type->kind == TYPE_INT || type->kind == TYPE_FLOAT);
}
static bool type_copyable(Type *type) {
  if (!type)
    return false;
  switch (type->kind) {
  case TYPE_NIL:
  case TYPE_INT:
  case TYPE_FLOAT:
  case TYPE_BOOL:
  case TYPE_STRING:
  case TYPE_BYTES:
  case TYPE_ENUM:
    return true;
  case TYPE_ARRAY:
  case TYPE_OPTION:
    return type_copyable(type->first);
  case TYPE_RESULT:
    return type_copyable(type->first) && type_copyable(type->second);
  case TYPE_UNKNOWN:
  case TYPE_STRUCT:
    return struct_type_copyable(type->structure);
  case TYPE_CHANNEL:
  case TYPE_THREAD:
  case TYPE_ERROR:
  case TYPE_VOID:
    return false;
  }
  return false;
}
static const char *type_label(Type *type) {
  return type && type->name ? type->name : "<unknown>";
}
static bool type_known(Type *type) {
  if (!type || type->kind == TYPE_UNKNOWN || type->kind == TYPE_ERROR)
    return false;
  if ((type->kind == TYPE_ARRAY || type->kind == TYPE_OPTION ||
       type->kind == TYPE_CHANNEL || type->kind == TYPE_THREAD) &&
      !type_known(type->first))
    return false;
  if (type->kind == TYPE_RESULT &&
      (!type_known(type->first) || !type_known(type->second)))
    return false;
  return true;
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
  bool resolved;
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
static bool struct_type_copyable(StructDecl *structure) {
  if (!structure)
    return false;
  for (size_t i = 0; i < structure->field_count; i++)
    if (!type_copyable(structure->fields[i].type))
      return false;
  return true;
}
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
  size_t string_length;
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
  size_t text_length;
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
  bool is_worker;
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
  size_t path_length;
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
  Source **sources;
  size_t source_count;
  size_t source_capacity;
} Program;
static void program_add_source(Program *program, Source *source) {
  for (size_t i = 0; i < program->source_count; i++)
    if (program->sources[i] == source ||
        !strcmp(program->sources[i]->name, source->name))
      return;
  program->sources = agrow(program->sources, program->source_count,
                           &program->source_capacity, program->source_count + 1,
                           sizeof(Source *));
  program->sources[program->source_count++] = source;
}

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
static bool parse_integer(Parser *p, Token token, int64_t *value) {
  char *text = tok_text(p, token);
  errno = 0;
  char *end = NULL;
  intmax_t parsed = strtoimax(text, &end, 10);
  if (errno == ERANGE || !end || end == text || *end ||
      parsed > (intmax_t)INT64_MAX || parsed < (intmax_t)INT64_MIN) {
    parse_fail(p, &token,
               "integer literal is outside the supported Int range");
    return false;
  }
  *value = (int64_t)parsed;
  return true;
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
static char *decode_string(Parser *p, Token token, size_t *decoded_length) {
  const char *raw = token.source->text + token.start + 1;
  size_t length = token.length >= 2 ? token.length - 2 : 0;
  size_t allocation = 0;
  if (!size_add(length, 1, &allocation)) {
    parse_fail(p, &token, "string literal is too large");
    if (decoded_length)
      *decoded_length = 0;
    return astr("");
  }
  char *out = aalloc(allocation);
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
  if (decoded_length)
    *decoded_length = used;
  return out;
}

static Expr *parse_primary(Parser *p) {
  Token token = *peek(p);
  if (match_tok(p, TOK_INT)) {
    Expr *e = expr_new(p, EX_INT, token);
    if (!parse_integer(p, token, &e->as.integer))
      e->as.integer = 0;
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
    e->as.string = decode_string(p, token, &e->string_length);
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
      NULL,         0,    NULL,       NULL,       NULL,       false};
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
    pattern.kind = PAT_INT;
    if (!parse_integer(p, token, &pattern.integer))
      pattern.integer = 0;
    return pattern;
  }
  if (match_tok(p, TOK_STRING)) {
    pattern.kind = PAT_STRING;
    pattern.text = decode_string(p, token, &pattern.text_length);
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
    Field value = {tok_text(p, field), NULL, false, field.source, field.line,
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
      size_t import_length = 0;
      ImportDecl value = {decode_string(&p, path, &import_length), import_length,
                          path.source, path.line, path.column};
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
  bool worker_context;
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
    return type_array(&t_unknown);
  if (!strcmp(spec->name, "Array") && spec->parameter_count == 1)
    return type_array(resolve_type(program, spec->parameters[0], error));
  if (!strcmp(spec->name, "Option") && spec->parameter_count == 1)
    return type_option(resolve_type(program, spec->parameters[0], error));
  if (!strcmp(spec->name, "Result") && spec->parameter_count == 2)
    return type_result(resolve_type(program, spec->parameters[0], error),
                       resolve_type(program, spec->parameters[1], error));
  if (!strcmp(spec->name, "Channel") && spec->parameter_count == 1)
    return type_channel(resolve_type(program, spec->parameters[0], error));
  if (!strcmp(spec->name, "Thread") && spec->parameter_count == 1)
    return type_thread(resolve_type(program, spec->parameters[0], error));
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
  B_MIN,
  B_MAX,
  B_FLOOR,
  B_CEIL,
  B_ROUND,
  B_POW,
  B_LOG,
  B_SIN,
  B_COS,
  B_IS_NAN,
  B_IS_FINITE,
  B_IS_SOME,
  B_IS_NONE,
  B_IS_OK,
  B_IS_ERR,
  B_UNWRAP_OR,
  B_SOME,
  B_NONE,
  B_OK,
  B_ERR,
  B_SUBSTRING,
  B_CONTAINS,
  B_STARTS_WITH,
  B_ENDS_WITH,
  B_TRIM,
  B_SPLIT,
  B_REPLACE,
  B_CODEPOINTS,
  B_BYTE_AT,
  B_HEX_ENCODE,
  B_HEX_DECODE,
  B_BASE64_ENCODE,
  B_BASE64_DECODE,
  B_ARRAY_POP,
  B_ARRAY_GET,
  B_ARRAY_CONCAT,
  B_ARRAY_CONTAINS,
  B_ARRAY_SLICE,
  B_ARRAY_REVERSE,
  B_ARRAY_JOIN,
  B_THREAD_CHANNEL,
  B_THREAD_CHANNEL_WITH_CAPACITY,
  B_THREAD_SPAWN,
  B_THREAD_SEND,
  B_THREAD_TRY_SEND,
  B_THREAD_SEND_TIMEOUT,
  B_THREAD_RECEIVE,
  B_THREAD_TRY_RECEIVE,
  B_THREAD_RECEIVE_TIMEOUT,
  B_THREAD_JOIN,
  B_THREAD_JOIN_TIMEOUT,
  B_THREAD_CANCEL,
  B_THREAD_CLOSE,
  B_FS_READ_TEXT,
  B_FS_WRITE_TEXT,
  B_FS_READ_BYTES,
  B_FS_WRITE_BYTES,
  B_FS_EXISTS,
  B_ENV_GET,
  B_COUNT
} BuiltinId;
typedef struct {
  const char *name;
  const char *signature;
  int arity;
  BuiltinId id;
  const char *failure;
  const char *description;
} BuiltinSpec;
static const BuiltinSpec builtins[] = {
    {"print", "print(value: Display) -> Nil", 1, B_PRINT,
     "I/O failure is reported by the host stream.",
     "Write one value without a newline."},
    {"println", "println(value: Display) -> Nil", 1, B_PRINTLN,
     "I/O failure is reported by the host stream.",
     "Write one value followed by a newline."},
    {"len", "len(value: String|Array[T]|Bytes) -> Int", 1, B_LEN,
     "Rejects values outside the supported collection types.",
     "Count String code points, Array elements, or Bytes."},
    {"bytes", "bytes(value: Array[Int]) -> Bytes", 1, B_BYTES,
     "Rejects elements outside the inclusive range 0..255.",
     "Convert Array[Int] octets to Bytes."},
    {"string_to_bytes", "string_to_bytes(value: String) -> Bytes", 1,
     B_STRING_TO_BYTES, "Rejects invalid UTF-8.", "Encode a String as UTF-8 Bytes."},
    {"bytes_to_string", "bytes_to_string(value: Bytes) -> String", 1,
     B_BYTES_TO_STRING, "Rejects invalid UTF-8.", "Decode valid UTF-8 Bytes as String."},
    {"array_push", "array_push(array: Array[T], value: T) -> Array[T]", 2,
     B_ARRAY_PUSH, "Rejects a mismatched element or allocation overflow.",
     "Return an Array with one element appended."},
    {"int", "int(value: Int|Float|Bool|String) -> Int", 1, B_INT,
     "Rejects incomplete, malformed, non-finite, or out-of-range values.",
     "Explicitly convert Int, Float, Bool, or a complete decimal String."},
    {"float", "float(value: Int|Float|String) -> Float", 1, B_FLOAT,
     "Rejects incomplete, malformed, or non-finite values.",
     "Explicitly convert Int, Float, or a complete decimal String."},
    {"str", "str(value: Display) -> String", 1, B_STR,
     "Always returns deterministic display text.",
     "Convert any value to its deterministic display String."},
    {"bool", "bool(value: Display) -> Bool", 1, B_BOOL,
     "Uses only explicit conversion rules.",
     "Explicitly convert a scalar or collection to Bool."},
    {"assert", "assert(condition: Bool) -> Nil", 1, B_ASSERT,
     "A false condition raises a runtime assertion error.",
     "Require a Bool condition."},
    {"assert_eq", "assert_eq(left: T, right: T) -> Nil", 2, B_ASSERT_EQ,
     "Unequal values raise a runtime assertion error.",
     "Require two equal values."},
    {"abs", "abs(value: Int|Float) -> Int|Float", 1, B_ABS,
     "Rejects Int minimum and non-finite Float values.",
     "Checked absolute value for Int or Float."},
    {"sqrt", "sqrt(value: Int|Float) -> Float", 1, B_SQRT,
     "Rejects negative or non-finite values.",
     "Compute a finite non-negative square root."},
    {"min", "min(left: Int|Float, right: Int|Float) -> Int|Float", 2, B_MIN,
     "Requires matching finite numeric types.", "Return the smaller number."},
    {"max", "max(left: Int|Float, right: Int|Float) -> Int|Float", 2, B_MAX,
     "Requires matching finite numeric types.", "Return the larger number."},
    {"floor", "floor(value: Float) -> Int", 1, B_FLOOR,
     "Rejects non-finite or out-of-range results.", "Round a Float toward negative infinity."},
    {"ceil", "ceil(value: Float) -> Int", 1, B_CEIL,
     "Rejects non-finite or out-of-range results.", "Round a Float toward positive infinity."},
    {"round", "round(value: Float) -> Int", 1, B_ROUND,
     "Rejects non-finite or out-of-range results.", "Round a Float to the nearest Int."},
    {"pow", "pow(base: Float, exponent: Float) -> Float", 2, B_POW,
     "Rejects non-finite results.", "Compute a finite floating-point power."},
    {"log", "log(value: Float) -> Float", 1, B_LOG,
     "Rejects non-positive or non-finite values.", "Compute a finite natural logarithm."},
    {"sin", "sin(value: Float) -> Float", 1, B_SIN,
     "Rejects non-finite results.", "Compute a finite sine."},
    {"cos", "cos(value: Float) -> Float", 1, B_COS,
     "Rejects non-finite results.", "Compute a finite cosine."},
    {"is_nan", "is_nan(value: Float) -> Bool", 1, B_IS_NAN,
     "Requires a Float.", "Test the IEEE NaN predicate."},
    {"is_finite", "is_finite(value: Float) -> Bool", 1, B_IS_FINITE,
     "Requires a Float.", "Test whether a Float is finite."},
    {"is_some", "is_some(value: Option[T]) -> Bool", 1, B_IS_SOME,
     "Requires Option[T].", "Test whether an Option is present."},
    {"is_none", "is_none(value: Option[T]) -> Bool", 1, B_IS_NONE,
     "Requires Option[T].", "Test whether an Option is empty."},
    {"is_ok", "is_ok(value: Result[T, E]) -> Bool", 1, B_IS_OK,
     "Requires Result[T, E].", "Test whether a Result is successful."},
    {"is_err", "is_err(value: Result[T, E]) -> Bool", 1, B_IS_ERR,
     "Requires Result[T, E].", "Test whether a Result is an error."},
    {"unwrap_or", "unwrap_or(value: Option[T], fallback: T) -> T", 2, B_UNWRAP_OR,
     "Requires a matching Option fallback type.", "Extract an Option or use a fallback."},
    {"substring", "substring(value: String, start: Int, length: Int) -> Result[String, String]", 3, B_SUBSTRING,
     "Rejects invalid UTF-8 and out-of-range spans.", "Extract a UTF-8 code-point substring."},
    {"contains", "contains(value: String, needle: String) -> Bool", 2, B_CONTAINS,
     "Requires String operands.", "Test whether a String contains a substring."},
    {"starts_with", "starts_with(value: String, prefix: String) -> Bool", 2, B_STARTS_WITH,
     "Requires String operands.", "Test a String prefix."},
    {"ends_with", "ends_with(value: String, suffix: String) -> Bool", 2, B_ENDS_WITH,
     "Requires String operands.", "Test a String suffix."},
    {"trim", "trim(value: String) -> String", 1, B_TRIM,
     "Requires valid UTF-8.", "Trim ASCII whitespace from both ends."},
    {"split", "split(value: String, separator: String) -> Array[String]", 2, B_SPLIT,
     "Requires String operands.", "Split a String deterministically."},
    {"replace", "replace(value: String, from: String, to: String) -> String", 3, B_REPLACE,
     "Requires String operands.", "Replace all non-overlapping occurrences."},
    {"codepoints", "codepoints(value: String) -> Array[Int]", 1, B_CODEPOINTS,
     "Rejects invalid UTF-8.", "Return Unicode scalar values."},
    {"byte_at", "byte_at(value: String, index: Int) -> Result[Int, String]", 2, B_BYTE_AT,
     "Rejects invalid UTF-8 or out-of-range indexes.", "Read a UTF-8 byte by byte position."},
    {"hex_encode", "hex_encode(value: Bytes) -> String", 1, B_HEX_ENCODE,
     "Always succeeds.", "Encode bytes as lowercase hexadecimal."},
    {"hex_decode", "hex_decode(value: String) -> Result[Bytes, String]", 1, B_HEX_DECODE,
     "Rejects odd or non-hex text.", "Decode lowercase or uppercase hexadecimal."},
    {"base64_encode", "base64_encode(value: Bytes) -> String", 1, B_BASE64_ENCODE,
     "Always succeeds.", "Encode bytes as standard base64."},
    {"base64_decode", "base64_decode(value: String) -> Result[Bytes, String]", 1, B_BASE64_DECODE,
     "Rejects malformed base64.", "Decode standard base64 text."},
    {"array_pop", "array_pop(value: Array[T]) -> Option[T]", 1, B_ARRAY_POP,
     "Empty arrays produce none.", "Return the last Array element."},
    {"array_get", "array_get(value: Array[T], index: Int) -> Option[T]", 2, B_ARRAY_GET,
     "Out-of-range indexes produce none.", "Read an Array element safely."},
    {"array_concat", "array_concat(left: Array[T], right: Array[T]) -> Array[T]", 2, B_ARRAY_CONCAT,
     "Requires matching element types.", "Concatenate two Arrays."},
    {"array_contains", "array_contains(value: Array[T], needle: T) -> Bool", 2, B_ARRAY_CONTAINS,
     "Requires a matching element type.", "Test Array membership."},
    {"array_slice", "array_slice(value: Array[T], start: Int, length: Int) -> Array[T]", 3, B_ARRAY_SLICE,
     "Rejects invalid ranges.", "Copy a bounded Array slice."},
    {"array_reverse", "array_reverse(value: Array[T]) -> Array[T]", 1, B_ARRAY_REVERSE,
     "Always succeeds.", "Return an Array in reverse order."},
    {"array_join", "array_join(value: Array[String], separator: String) -> String", 2, B_ARRAY_JOIN,
     "Requires Array[String].", "Join text elements."},
    {"some", "some(value: T) -> Option[T]", 1, B_SOME,
     "Always constructs a present option.",
     "Construct Option[T] containing a value."},
    {"none", "none() -> Option[T]", 0, B_NONE,
     "Requires an Option[T] type context.", "Construct an empty Option[T]."},
    {"ok", "ok(value: T) -> Result[T, E]", 1, B_OK,
     "The error type is inferred from a Result[T, E] context or Nil.",
     "Construct Result[T, E] containing a success value."},
    {"err", "err(value: E) -> Result[T, E]", 1, B_ERR,
     "The success type is inferred from a Result[T, E] context or Nil.",
     "Construct Result[T, E] containing an error value."},
    {"thread_channel", "thread_channel() -> Channel[T]", 0, B_THREAD_CHANNEL,
     "Requires a Channel[T] type context.",
     "Create an unbounded FIFO channel for copy-safe values."},
    {"thread_channel_with_capacity", "thread_channel_with_capacity(capacity: Int) -> Channel[T]", 1, B_THREAD_CHANNEL_WITH_CAPACITY,
     "Requires a positive capacity.", "Create a bounded FIFO channel."},
    {"thread_spawn", "thread_spawn(name: String) -> Thread[T]", 1,
     B_THREAD_SPAWN, "Requires the name of a zero-argument function.",
     "Start an OS-backed thread running a named function."},
        {"thread_send", "thread_send(channel: Channel[T], value: T) -> Nil", 2, B_THREAD_SEND,
     "Only Copy values may cross the channel.",
     "Send a copy-safe value, waiting while a bounded channel is full."},
    {"thread_try_send", "thread_try_send(channel: Channel[T], value: T) -> Result[Nil, String]", 2, B_THREAD_TRY_SEND,
     "Returns full or closed rather than blocking.", "Attempt one channel send."},
    {"thread_send_timeout", "thread_send_timeout(channel: Channel[T], value: T, milliseconds: Int) -> Result[Nil, String]", 3, B_THREAD_SEND_TIMEOUT,
     "Returns timeout or closed deterministically.", "Send with a bounded deadline."},
        {"thread_receive", "thread_receive(channel: Channel[T]) -> T", 1, B_THREAD_RECEIVE,
     "Receiving from a closed empty channel is an error.",
     "Receive one value, waiting while the channel is empty."},
    {"thread_try_receive", "thread_try_receive(channel: Channel[T]) -> Result[T, String]", 1, B_THREAD_TRY_RECEIVE,
     "Returns empty or closed rather than blocking.", "Attempt one channel receive."},
    {"thread_receive_timeout",
     "thread_receive_timeout(channel: Channel[T], milliseconds: Int) -> T", 2,
     B_THREAD_RECEIVE_TIMEOUT,
     "Rejects negative durations and reports a timeout without closing the channel.",
     "Receive a value with a bounded millisecond deadline."},
        {"thread_join", "thread_join(thread: Thread[T]) -> Nil", 1, B_THREAD_JOIN,
     "Worker failure is re-raised at join.",
     "Wait for a worker and propagate its failure."},
    {"thread_join_timeout", "thread_join_timeout(thread: Thread[T], milliseconds: Int) -> Result[Nil, String]", 2, B_THREAD_JOIN_TIMEOUT,
     "Returns timeout without detaching the worker.", "Join a worker with a bounded deadline."},
    {"thread_cancel", "thread_cancel(thread: Thread[T]) -> Nil", 1, B_THREAD_CANCEL,
     "Requests cooperative cancellation.", "Request cancellation of a worker."},
    {"thread_close", "thread_close(channel: Channel[T]) -> Nil", 1,
     B_THREAD_CLOSE, "Closing an already closed channel is harmless.",
     "Close a channel and wake blocked operations."},
    {"fs_read_text", "fs_read_text(path: String) -> Result[String, String]", 1,
     B_FS_READ_TEXT, "Rejects NUL paths and reports open or read failures.",
     "Read a UTF-8 text file into a typed Result."},
    {"fs_write_text",
     "fs_write_text(path: String, text: String) -> Result[Nil, String]", 2,
     B_FS_WRITE_TEXT, "Rejects NUL paths and reports write or close failures.",
     "Write UTF-8 text bytes and return a typed Result."},
    {"fs_read_bytes", "fs_read_bytes(path: String) -> Result[Bytes, String]", 1, B_FS_READ_BYTES,
     "Rejects NUL paths and reports I/O failures.", "Read raw file bytes."},
    {"fs_write_bytes", "fs_write_bytes(path: String, data: Bytes) -> Result[Nil, String]", 2, B_FS_WRITE_BYTES,
     "Rejects NUL paths and reports I/O failures.", "Write raw file bytes."},
    {"fs_exists", "fs_exists(path: String) -> Bool", 1, B_FS_EXISTS,
     "Rejects NUL paths.", "Test whether a filesystem path exists."},
    {"env_get", "env_get(name: String) -> Option[String]", 1, B_ENV_GET,
     "A missing variable produces none; NUL names are rejected.",
     "Read an environment variable into an explicit Option."}};
static const size_t builtin_count = sizeof(builtins) / sizeof(builtins[0]);
typedef struct {
  const char *effects;
  const char *ownership;
  const char *failure;
  const char *checker_rule;
  const char *runtime_callback;
  const char *version;
} BuiltinContract;
#define BUILTIN_CONTRACT(EFFECTS, OWNERSHIP) \
  {EFFECTS, OWNERSHIP, "typed diagnostic or Result", "check_call", \
   "builtin_runtime", KRY_VERSION}
static const BuiltinContract builtin_contracts[B_COUNT] = {
    [B_PRINT] = BUILTIN_CONTRACT("io", "borrow"),
    [B_PRINTLN] = BUILTIN_CONTRACT("io", "borrow"),
    [B_LEN] = BUILTIN_CONTRACT("pure", "copy"),
    [B_BYTES] = BUILTIN_CONTRACT("pure", "copy"),
    [B_STRING_TO_BYTES] = BUILTIN_CONTRACT("pure", "copy"),
    [B_BYTES_TO_STRING] = BUILTIN_CONTRACT("pure", "copy"),
    [B_ARRAY_PUSH] = BUILTIN_CONTRACT("pure", "copy"),
    [B_INT] = BUILTIN_CONTRACT("pure", "copy"),
    [B_FLOAT] = BUILTIN_CONTRACT("pure", "copy"),
    [B_STR] = BUILTIN_CONTRACT("pure", "copy"),
    [B_BOOL] = BUILTIN_CONTRACT("pure", "copy"),
    [B_ASSERT] = BUILTIN_CONTRACT("diagnostic", "borrow"),
    [B_ASSERT_EQ] = BUILTIN_CONTRACT("diagnostic", "borrow"),
    [B_ABS] = BUILTIN_CONTRACT("pure", "copy"),
    [B_SQRT] = BUILTIN_CONTRACT("pure", "copy"),
    [B_MIN] = BUILTIN_CONTRACT("pure", "copy"),
    [B_MAX] = BUILTIN_CONTRACT("pure", "copy"),
    [B_FLOOR] = BUILTIN_CONTRACT("pure", "copy"),
    [B_CEIL] = BUILTIN_CONTRACT("pure", "copy"),
    [B_ROUND] = BUILTIN_CONTRACT("pure", "copy"),
    [B_POW] = BUILTIN_CONTRACT("pure", "copy"),
    [B_LOG] = BUILTIN_CONTRACT("pure", "copy"),
    [B_SIN] = BUILTIN_CONTRACT("pure", "copy"),
    [B_COS] = BUILTIN_CONTRACT("pure", "copy"),
    [B_IS_NAN] = BUILTIN_CONTRACT("pure", "copy"),
    [B_IS_FINITE] = BUILTIN_CONTRACT("pure", "copy"),
    [B_IS_SOME] = BUILTIN_CONTRACT("pure", "copy"),
    [B_IS_NONE] = BUILTIN_CONTRACT("pure", "copy"),
    [B_IS_OK] = BUILTIN_CONTRACT("pure", "copy"),
    [B_IS_ERR] = BUILTIN_CONTRACT("pure", "copy"),
    [B_UNWRAP_OR] = BUILTIN_CONTRACT("pure", "copy"),
    [B_SOME] = BUILTIN_CONTRACT("pure", "copy"),
    [B_NONE] = BUILTIN_CONTRACT("pure", "copy"),
    [B_OK] = BUILTIN_CONTRACT("pure", "copy"),
    [B_ERR] = BUILTIN_CONTRACT("pure", "copy"),
    [B_SUBSTRING] = BUILTIN_CONTRACT("pure", "copy"),
    [B_CONTAINS] = BUILTIN_CONTRACT("pure", "copy"),
    [B_STARTS_WITH] = BUILTIN_CONTRACT("pure", "copy"),
    [B_ENDS_WITH] = BUILTIN_CONTRACT("pure", "copy"),
    [B_TRIM] = BUILTIN_CONTRACT("pure", "copy"),
    [B_SPLIT] = BUILTIN_CONTRACT("pure", "copy"),
    [B_REPLACE] = BUILTIN_CONTRACT("pure", "copy"),
    [B_CODEPOINTS] = BUILTIN_CONTRACT("pure", "copy"),
    [B_BYTE_AT] = BUILTIN_CONTRACT("pure", "copy"),
    [B_HEX_ENCODE] = BUILTIN_CONTRACT("pure", "copy"),
    [B_HEX_DECODE] = BUILTIN_CONTRACT("pure", "copy"),
    [B_BASE64_ENCODE] = BUILTIN_CONTRACT("pure", "copy"),
    [B_BASE64_DECODE] = BUILTIN_CONTRACT("pure", "copy"),
    [B_ARRAY_POP] = BUILTIN_CONTRACT("pure", "copy"),
    [B_ARRAY_GET] = BUILTIN_CONTRACT("pure", "copy"),
    [B_ARRAY_CONCAT] = BUILTIN_CONTRACT("pure", "copy"),
    [B_ARRAY_CONTAINS] = BUILTIN_CONTRACT("pure", "copy"),
    [B_ARRAY_SLICE] = BUILTIN_CONTRACT("pure", "copy"),
    [B_ARRAY_REVERSE] = BUILTIN_CONTRACT("pure", "copy"),
    [B_ARRAY_JOIN] = BUILTIN_CONTRACT("pure", "copy"),
    [B_THREAD_CHANNEL] = BUILTIN_CONTRACT("thread", "handle"),
    [B_THREAD_CHANNEL_WITH_CAPACITY] = BUILTIN_CONTRACT("thread", "handle"),
    [B_THREAD_SPAWN] = BUILTIN_CONTRACT("thread", "handle"),
    [B_THREAD_SEND] = BUILTIN_CONTRACT("thread", "clone"),
    [B_THREAD_TRY_SEND] = BUILTIN_CONTRACT("thread", "clone"),
    [B_THREAD_SEND_TIMEOUT] = BUILTIN_CONTRACT("thread", "clone"),
    [B_THREAD_RECEIVE] = BUILTIN_CONTRACT("thread", "clone"),
    [B_THREAD_TRY_RECEIVE] = BUILTIN_CONTRACT("thread", "clone"),
    [B_THREAD_RECEIVE_TIMEOUT] = BUILTIN_CONTRACT("thread", "clone"),
    [B_THREAD_JOIN] = BUILTIN_CONTRACT("thread", "handle"),
    [B_THREAD_JOIN_TIMEOUT] = BUILTIN_CONTRACT("thread", "handle"),
    [B_THREAD_CANCEL] = BUILTIN_CONTRACT("thread", "handle"),
    [B_THREAD_CLOSE] = BUILTIN_CONTRACT("thread", "handle"),
    [B_FS_READ_TEXT] = BUILTIN_CONTRACT("filesystem", "copy"),
    [B_FS_WRITE_TEXT] = BUILTIN_CONTRACT("filesystem", "borrow"),
    [B_FS_READ_BYTES] = BUILTIN_CONTRACT("filesystem", "copy"),
    [B_FS_WRITE_BYTES] = BUILTIN_CONTRACT("filesystem", "borrow"),
    [B_FS_EXISTS] = BUILTIN_CONTRACT("filesystem", "borrow"),
    [B_ENV_GET] = BUILTIN_CONTRACT("environment", "copy")};
static const size_t builtin_contract_count = sizeof(builtin_contracts) / sizeof(builtin_contracts[0]);
static bool builtin_registry_valid(void) {
  if (builtin_count != B_COUNT || builtin_contract_count != B_COUNT) return false;
  for (size_t i = 0; i < builtin_count; i++) {
    BuiltinId id = builtins[i].id;
    if (id >= B_COUNT || !builtin_contracts[id].effects ||
        !builtin_contracts[id].ownership || !builtin_contracts[id].failure ||
        !builtin_contracts[id].checker_rule || !builtin_contracts[id].runtime_callback ||
        !builtin_contracts[id].version) return false;
  }
  return true;
}
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
    if (scope->worker_context &&
        (builtin->id == B_FS_READ_TEXT || builtin->id == B_FS_WRITE_TEXT ||
         builtin->id == B_FS_READ_BYTES || builtin->id == B_FS_WRITE_BYTES ||
         builtin->id == B_FS_EXISTS || builtin->id == B_ENV_GET ||
         builtin->id == B_THREAD_SPAWN)) {
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "builtin '%s' is not available in a worker-safe function", name);
      return &t_error;
    }
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
      Type *value = check_expr(
          program, scope, expr->as.call.args[1],
          first->first->kind == TYPE_UNKNOWN ? NULL : first->first, error);
      if (first->first->kind == TYPE_UNKNOWN)
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
    case B_MIN:
    case B_MAX: {
      Type *second = check_expr(program, scope, expr->as.call.args[1], first, error);
      if (!error->set && (!type_numeric(first) || !type_equal(first, second)))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "min/max require matching Int or Float operands");
      return first;
    }
    case B_FLOOR:
    case B_CEIL:
    case B_ROUND:
      if (!type_equal(first, &t_float)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "floor, ceil, and round expect Float");
        return &t_error;
      }
      return &t_int;
    case B_POW:
      if (!type_equal(first, &t_float)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "pow expects Float operands");
        return &t_error;
      }
      if (!type_equal(check_expr(program, scope, expr->as.call.args[1], &t_float,
                                 error), &t_float) && !error->set)
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "pow expects Float operands");
      return &t_float;
    case B_LOG:
    case B_SIN:
    case B_COS:
      if (!type_equal(first, &t_float)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "math function expects Float");
        return &t_error;
      }
      return &t_float;
    case B_IS_NAN:
    case B_IS_FINITE:
      if (!type_equal(first, &t_float)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "is_nan/is_finite expect Float");
        return &t_error;
      }
      return &t_bool;
    case B_IS_SOME:
    case B_IS_NONE:
      if (first->kind != TYPE_OPTION || !type_known(first->first)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "is_some/is_none expect Option[T]");
        return &t_error;
      }
      return &t_bool;
    case B_IS_OK:
    case B_IS_ERR:
      if (first->kind != TYPE_RESULT || !type_known(first->first) ||
          !type_known(first->second)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "is_ok/is_err expect Result[T, E]");
        return &t_error;
      }
      return &t_bool;
    case B_UNWRAP_OR: {
      if (first->kind != TYPE_OPTION || !type_known(first->first)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "unwrap_or expects Option[T]");
        return &t_error;
      }
      Type *fallback = check_expr(program, scope, expr->as.call.args[1],
                                  first->first, error);
      if (!error->set && !type_equal(fallback, first->first))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "unwrap_or fallback must match Option[T]");
      return first->first;
    }
    case B_SUBSTRING: {
      if (!type_equal(first, &t_string)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "substring expects String");
        return &t_error;
      }
      Type *start = check_expr(program, scope, expr->as.call.args[1], &t_int, error);
      Type *length = check_expr(program, scope, expr->as.call.args[2], &t_int, error);
      if (!error->set && (!type_equal(start, &t_int) || !type_equal(length, &t_int)))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "substring indexes must be Int");
      return type_result(&t_string, &t_string);
    }
    case B_CONTAINS:
    case B_STARTS_WITH:
    case B_ENDS_WITH:
      if (!type_equal(first, &t_string) ||
          !type_equal(check_expr(program, scope, expr->as.call.args[1], &t_string,
                                 error), &t_string))
        if (!error->set)
          error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                    "text predicate expects String operands");
      return &t_bool;
    case B_TRIM:
    case B_CODEPOINTS:
    case B_HEX_ENCODE:
    case B_BASE64_ENCODE:
      if ((builtin->id == B_TRIM || builtin->id == B_CODEPOINTS) &&
          !type_equal(first, &t_string))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "text operation expects String");
      else if ((builtin->id == B_HEX_ENCODE || builtin->id == B_BASE64_ENCODE) &&
               !type_equal(first, &t_bytes))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "encoding operation expects Bytes");
      return builtin->id == B_CODEPOINTS ? type_array(&t_int) : &t_string;
    case B_SPLIT:
      if (!type_equal(first, &t_string) ||
          !type_equal(check_expr(program, scope, expr->as.call.args[1], &t_string,
                                 error), &t_string))
        if (!error->set)
          error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                    "split expects String operands");
      return type_array(&t_string);
    case B_REPLACE:
      for (size_t i = 1; i < 3; i++)
        if (!type_equal(check_expr(program, scope, expr->as.call.args[i], &t_string,
                                  error), &t_string) && !error->set)
          error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                    "replace expects String operands");
      return &t_string;
    case B_BYTE_AT:
      if (!type_equal(first, &t_string) ||
          !type_equal(check_expr(program, scope, expr->as.call.args[1], &t_int,
                                 error), &t_int))
        if (!error->set)
          error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                    "byte_at expects String and Int");
      return type_result(&t_int, &t_string);
    case B_HEX_DECODE:
    case B_BASE64_DECODE:
      if (!type_equal(first, &t_string))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "decoding operation expects String");
      return type_result(&t_bytes, &t_string);
    case B_ARRAY_POP:
      if (first->kind != TYPE_ARRAY || !type_known(first->first))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "array_pop expects Array[T]");
      return first->kind == TYPE_ARRAY ? type_option(first->first) : &t_error;
    case B_ARRAY_GET:
      if (first->kind != TYPE_ARRAY || !type_known(first->first) ||
          !type_equal(check_expr(program, scope, expr->as.call.args[1], &t_int,
                                 error), &t_int))
        if (!error->set)
          error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                    "array_get expects Array[T] and Int");
      return first->kind == TYPE_ARRAY ? type_option(first->first) : &t_error;
    case B_ARRAY_CONCAT: {
      if (first->kind != TYPE_ARRAY || !type_known(first->first)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "array_concat expects Array[T]");
        return &t_error;
      }
      Type *second = check_expr(program, scope, expr->as.call.args[1], first, error);
      if (!error->set && !type_equal(first, second))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "array_concat requires matching Array types");
      return first;
    }
    case B_ARRAY_CONTAINS: {
      if (first->kind != TYPE_ARRAY || !type_known(first->first)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "array_contains expects Array[T]");
        return &t_error;
      }
      Type *needle = check_expr(program, scope, expr->as.call.args[1], first->first,
                                error);
      if (!error->set && !type_equal(needle, first->first))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "array_contains needle type mismatch");
      return &t_bool;
    }
    case B_ARRAY_SLICE: {
      if (first->kind != TYPE_ARRAY || !type_known(first->first)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "array_slice expects Array[T]");
        return &t_error;
      }
      for (size_t i = 1; i < 3; i++)
        if (!type_equal(check_expr(program, scope, expr->as.call.args[i], &t_int,
                                   error), &t_int) && !error->set)
          error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                    "array_slice indexes must be Int");
      return first;
    }
    case B_ARRAY_REVERSE:
      if (first->kind != TYPE_ARRAY || !type_known(first->first))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "array_reverse expects Array[T]");
      return first;
    case B_ARRAY_JOIN:
      if (first->kind != TYPE_ARRAY || !type_equal(first->first, &t_string) ||
          !type_equal(check_expr(program, scope, expr->as.call.args[1], &t_string,
                                 error), &t_string))
        if (!error->set)
          error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                    "array_join expects Array[String] and String");
      return &t_string;
    case B_THREAD_CHANNEL_WITH_CAPACITY:
      if (!type_equal(first, &t_int))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread_channel_with_capacity expects Int");
      if (expected && expected->kind == TYPE_CHANNEL)
        return expected;
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "thread_channel_with_capacity requires a Channel[T] context");
      return &t_error;
    case B_THREAD_TRY_SEND:
    case B_THREAD_SEND_TIMEOUT: {
      if (first->kind != TYPE_CHANNEL || !type_known(first->first)) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread send expects Channel[T]");
        return &t_error;
      }
      Type *value = check_expr(program, scope, expr->as.call.args[1], first->first,
                               error);
      if (!error->set && !type_equal(value, first->first))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread send value type mismatch");
      if (builtin->id == B_THREAD_SEND_TIMEOUT &&
          !type_equal(check_expr(program, scope, expr->as.call.args[2], &t_int,
                                 error), &t_int) && !error->set)
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread_send_timeout duration must be Int");
      return type_result(&t_nil, &t_string);
    }
    case B_THREAD_TRY_RECEIVE:
      if (first->kind != TYPE_CHANNEL || !type_known(first->first))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread_try_receive expects Channel[T]");
      return first->kind == TYPE_CHANNEL ? type_result(first->first, &t_string)
                                         : &t_error;
    case B_THREAD_JOIN_TIMEOUT:
      if (first->kind != TYPE_THREAD || !type_known(first->first) ||
          !type_equal(check_expr(program, scope, expr->as.call.args[1], &t_int,
                                 error), &t_int))
        if (!error->set)
          error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                    "thread_join_timeout expects Thread[T] and Int");
      return type_result(&t_nil, &t_string);
    case B_THREAD_CANCEL:
      if (first->kind != TYPE_THREAD)
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread_cancel expects Thread[T]");
      return &t_nil;
    case B_FS_READ_BYTES:
      if (!type_equal(first, &t_string))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "fs_read_bytes expects a String path");
      return type_result(&t_bytes, &t_string);
    case B_FS_WRITE_BYTES:
      if (!type_equal(first, &t_string) ||
          !type_equal(check_expr(program, scope, expr->as.call.args[1], &t_bytes,
                                 error), &t_bytes))
        if (!error->set)
          error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                    "fs_write_bytes expects String and Bytes");
      return type_result(&t_nil, &t_string);
    case B_FS_EXISTS:
      if (!type_equal(first, &t_string))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "fs_exists expects a String path");
      return &t_bool;
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
    case B_THREAD_CHANNEL:
      if (expected && expected->kind == TYPE_CHANNEL)
        return expected;
      error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                "thread_channel requires a Channel[T] type context");
      return &t_error;
    case B_THREAD_SPAWN: {
      if (first->kind != TYPE_STRING) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread_spawn expects a worker function name String");
        return &t_error;
      }
      Expr *name_expr = expr->as.call.args[0];
      if (name_expr->kind != EX_STRING) {
        error_set(error, ERR_TYPE, name_expr->source, name_expr->line,
                  name_expr->column,
                  "thread_spawn requires a string literal worker name");
        return &t_error;
      }
      if (memchr(name_expr->as.string, '\0', name_expr->string_length)) {
        error_set(error, ERR_TYPE, name_expr->source, name_expr->line,
                  name_expr->column, "thread worker name cannot contain NUL");
        return &t_error;
      }
      Type *worker_result = &t_nil;
      {
        Function *worker = find_function(program, name_expr->as.string);
        if (!worker) {
          error_set(error, ERR_TYPE, name_expr->source, name_expr->line,
                    name_expr->column, "unknown thread worker '%s'",
                    name_expr->as.string);
          return &t_error;
        }
        if (worker->parameter_count != 0) {
          error_set(error, ERR_TYPE, name_expr->source, name_expr->line,
                    name_expr->column,
                    "thread worker '%s' must take no arguments",
                    name_expr->as.string);
          return &t_error;
        }
        worker_result = resolve_type(program, worker->return_type, error);
      }
      Type *thread_type = type_thread(worker_result);
      if (!error->set && expected && !type_equal(expected, thread_type))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread_spawn returns %s, found %s", type_label(expected),
                  type_label(thread_type));
      return thread_type;
    }
    case B_THREAD_SEND: {
      if (first->kind != TYPE_CHANNEL || !first->first) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread_send expects Channel[T] as its first argument");
        return &t_error;
      }
      Type *value = check_expr(program, scope, expr->as.call.args[1],
                               first->first, error);
      if (!error->set && !type_equal(value, first->first))
        error_set(error, ERR_TYPE, expr->as.call.args[1]->source,
                  expr->as.call.args[1]->line, expr->as.call.args[1]->column,
                  "thread_send expected %s, found %s", type_label(first->first),
                  type_label(value));
      if (!error->set && !type_copyable(first->first))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread_send accepts only Copy values");
      return &t_nil;
    }
    case B_THREAD_RECEIVE:
      if (first->kind != TYPE_CHANNEL || !first->first) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread_receive expects Channel[T]");
        return &t_error;
      }
      return first->first;
    case B_THREAD_RECEIVE_TIMEOUT: {
      if (first->kind != TYPE_CHANNEL || !first->first) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread_receive_timeout expects Channel[T]");
        return &t_error;
      }
      Type *duration = check_expr(program, scope, expr->as.call.args[1],
                                  &t_int, error);
      if (!error->set && !type_equal(duration, &t_int))
        error_set(error, ERR_TYPE, expr->as.call.args[1]->source,
                  expr->as.call.args[1]->line, expr->as.call.args[1]->column,
                  "thread_receive_timeout duration must be Int milliseconds");
      Expr *duration_expr = expr->as.call.args[1];
      bool negative_literal =
          duration_expr->kind == EX_UNARY &&
          duration_expr->as.unary.op == TOK_MINUS &&
          duration_expr->as.unary.operand->kind == EX_INT &&
          duration_expr->as.unary.operand->as.integer > 0;
      if (!error->set &&
          ((duration_expr->kind == EX_INT && duration_expr->as.integer < 0) ||
           negative_literal))
        error_set(error, ERR_TYPE, duration_expr->source, duration_expr->line,
                  duration_expr->column,
                  "thread_receive_timeout duration cannot be negative");
      return first->first;
    }
    case B_THREAD_JOIN:
      if (first->kind != TYPE_THREAD) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread_join expects Thread[T]");
        return &t_error;
      }
      return &t_nil;
    case B_THREAD_CLOSE:
      if (first->kind != TYPE_CHANNEL) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "thread_close expects Channel[T]");
        return &t_error;
      }
      return &t_nil;
    case B_FS_READ_TEXT:
      if (first->kind != TYPE_STRING) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "fs_read_text expects a String path");
        return &t_error;
      }
      return type_result(&t_string, &t_string);
    case B_FS_WRITE_TEXT: {
      if (first->kind != TYPE_STRING) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "fs_write_text expects a String path");
        return &t_error;
      }
      Type *text = check_expr(program, scope, expr->as.call.args[1],
                              &t_string, error);
      if (!error->set && !type_equal(text, &t_string))
        error_set(error, ERR_TYPE, expr->as.call.args[1]->source,
                  expr->as.call.args[1]->line, expr->as.call.args[1]->column,
                  "fs_write_text expects String content");
      return type_result(&t_nil, &t_string);
    }
    case B_ENV_GET:
      if (first->kind != TYPE_STRING) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "env_get expects a String name");
        return &t_error;
      }
      return type_option(&t_string);
    case B_COUNT:
      return &t_error;
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
    size_t seen_bytes = 0;
    if (!size_mul(decl->field_count, sizeof(bool), &seen_bytes)) {
      error_set(error, ERR_RESOURCE, expr->source, expr->line, expr->column,
                "struct field validation size overflow");
      return &t_error;
    }
    bool *seen = decl->field_count ? aalloc(seen_bytes) : NULL;
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
      if (seen[field]) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "duplicate field '%s' in struct '%s'",
                  expr->as.structure.fields[i], decl->name);
        return &t_error;
      }
      seen[field] = true;
      Type *actual = check_expr(program, scope, expr->as.structure.values[i],
                                decl->fields[field].type, error);
      if (!error->set && !type_equal(actual, decl->fields[field].type))
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "field '%s' expected %s, found %s", decl->fields[field].name,
                  type_label(decl->fields[field].type), type_label(actual));
    }
    for (size_t field = 0; field < decl->field_count; field++)
      if (!seen[field]) {
        error_set(error, ERR_TYPE, expr->source, expr->line, expr->column,
                  "missing field '%s' in struct '%s'",
                  decl->fields[field].name, decl->name);
        return &t_error;
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
    if (scrutinee->kind != TYPE_NIL && scrutinee->kind != TYPE_OPTION)
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
      else if (declared->kind == TYPE_ARRAY && declared->first->kind == TYPE_UNKNOWN &&
               actual->kind == TYPE_ARRAY && actual->first->kind != TYPE_UNKNOWN)
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
      Scope then_scope = {NULL, scope, scope->worker_context},
            else_scope = {NULL, scope, scope->worker_context};
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
      Scope body_scope = {NULL, scope, scope->worker_context};
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
      bool bool_true = false, bool_false = false, nil_seen = false;
      bool option_some = false, option_none = false;
      bool result_ok = false, result_err = false;
      size_t enum_count = scrutinee->kind == TYPE_ENUM
                              ? scrutinee->enumeration->variant_count
                              : 0;
      size_t enum_seen_bytes = 0;
      if (!size_mul(enum_count, sizeof(bool), &enum_seen_bytes)) {
        error_set(error, ERR_RESOURCE, s->source, s->line, s->column,
                  "match validation size overflow");
        break;
      }
      bool *seen = enum_count ? aalloc(enum_seen_bytes) : NULL;
      for (size_t a = 0; a < s->as.match_stmt.arm_count && !error->set; a++) {
        Pattern *pattern = &s->as.match_stmt.arms[a].pattern;
        if (wildcard) {
          error_set(error, ERR_TYPE, pattern->source, pattern->line,
                    pattern->column, "unreachable match arm after '_'");
          break;
        }
        if (pattern->kind == PAT_WILDCARD)
          wildcard = true;
        Scope arm_scope = {NULL, scope, scope->worker_context};
        check_pattern(scrutinee, pattern, &arm_scope, error);
        if (pattern->kind == PAT_OPTION && scrutinee->kind == TYPE_OPTION) {
          bool *covered = pattern->ok ? &option_some : &option_none;
          if (*covered && !error->set)
            error_set(error, ERR_TYPE, pattern->source, pattern->line,
                      pattern->column, "duplicate match arm for Option variant");
          *covered = true;
        }
        if (pattern->kind == PAT_NIL && scrutinee->kind == TYPE_OPTION) {
          if (option_none && !error->set)
            error_set(error, ERR_TYPE, pattern->source, pattern->line,
                      pattern->column, "duplicate match arm for Option variant");
          option_none = true;
        }
        if (pattern->kind == PAT_NIL && scrutinee->kind == TYPE_NIL) {
          if (nil_seen && !error->set)
            error_set(error, ERR_TYPE, pattern->source, pattern->line,
                      pattern->column, "duplicate nil match arm");
          nil_seen = true;
        }
        if (pattern->kind == PAT_BOOL && scrutinee->kind == TYPE_BOOL) {
          bool *covered = pattern->boolean ? &bool_true : &bool_false;
          if (*covered && !error->set)
            error_set(error, ERR_TYPE, pattern->source, pattern->line,
                      pattern->column, "duplicate boolean match arm");
          *covered = true;
        }
        if (pattern->kind == PAT_RESULT && scrutinee->kind == TYPE_RESULT) {
          bool *covered = pattern->ok ? &result_ok : &result_err;
          if (*covered && !error->set)
            error_set(error, ERR_TYPE, pattern->source, pattern->line,
                      pattern->column, "duplicate match arm for Result variant");
          *covered = true;
        }
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
      if (!error->set && !wildcard && scrutinee->kind == TYPE_BOOL &&
          (!bool_true || !bool_false))
        error_set(error, ERR_TYPE, s->source, s->line, s->column,
                  "non-exhaustive match for Bool; add true and false or '_'");
      if (!error->set && !wildcard && scrutinee->kind == TYPE_NIL && !nil_seen)
        error_set(error, ERR_TYPE, s->source, s->line, s->column,
                  "non-exhaustive match for Nil; add nil or '_'");
      if (!error->set && !wildcard && scrutinee->kind == TYPE_ENUM) {
        for (size_t v = 0; v < enum_count; v++)
          if (!seen[v]) {
            error_set(error, ERR_TYPE, s->source, s->line, s->column,
                      "non-exhaustive match for enum '%s'; add '_'",
                      scrutinee->enumeration->name);
            break;
          }
      }
      if (!error->set && !wildcard && scrutinee->kind == TYPE_OPTION &&
          (!option_some || !option_none))
        error_set(error, ERR_TYPE, s->source, s->line, s->column,
                  "non-exhaustive match for Option; add some and none or '_'");
      if (!error->set && !wildcard && scrutinee->kind == TYPE_RESULT &&
          (!result_ok || !result_err))
        error_set(error, ERR_TYPE, s->source, s->line, s->column,
                  "non-exhaustive match for Result; add ok and err or '_'");
      break;
    }
    }
  }
  if (returns)
    *returns = did_return;
  return !error->set;
}
static void mark_worker_expr(Program *program, Expr *expr) {
  if (!expr)
    return;
  switch (expr->kind) {
  case EX_CALL:
    if (!strcmp(expr->as.call.name, "thread_spawn") &&
        expr->as.call.count == 1 && expr->as.call.args[0]->kind == EX_STRING) {
      Expr *name = expr->as.call.args[0];
      if (!memchr(name->as.string, '\0', name->string_length)) {
        Function *worker = find_function(program, name->as.string);
        if (worker)
          worker->is_worker = true;
      }
    }
    for (size_t i = 0; i < expr->as.call.count; i++)
      mark_worker_expr(program, expr->as.call.args[i]);
    break;
  case EX_UNARY:
    mark_worker_expr(program, expr->as.unary.operand);
    break;
  case EX_BINARY:
    mark_worker_expr(program, expr->as.binary.left);
    mark_worker_expr(program, expr->as.binary.right);
    break;
  case EX_ARRAY:
    for (size_t i = 0; i < expr->as.array.count; i++)
      mark_worker_expr(program, expr->as.array.items[i]);
    break;
  case EX_INDEX:
    mark_worker_expr(program, expr->as.index.base);
    mark_worker_expr(program, expr->as.index.index);
    break;
  case EX_FIELD:
    mark_worker_expr(program, expr->as.field.base);
    break;
  case EX_STRUCT:
    for (size_t i = 0; i < expr->as.structure.count; i++)
      mark_worker_expr(program, expr->as.structure.values[i]);
    break;
  case EX_OPTION:
    mark_worker_expr(program, expr->as.option.value);
    break;
  case EX_RESULT:
    mark_worker_expr(program, expr->as.result.value);
    break;
  case EX_INT:
  case EX_FLOAT:
  case EX_BOOL:
  case EX_NIL:
  case EX_STRING:
  case EX_VAR:
  case EX_ENUM:
    break;
  }
}
static void mark_worker_statements(Program *program, Stmt **statements,
                                   size_t count) {
  for (size_t i = 0; i < count; i++) {
    Stmt *stmt = statements[i];
    switch (stmt->kind) {
    case ST_LET:
      mark_worker_expr(program, stmt->as.let.initializer);
      break;
    case ST_EXPR:
      mark_worker_expr(program, stmt->as.expression);
      break;
    case ST_ASSIGN:
      mark_worker_expr(program, stmt->as.assign.target);
      mark_worker_expr(program, stmt->as.assign.value);
      break;
    case ST_IF:
      mark_worker_expr(program, stmt->as.if_stmt.condition);
      mark_worker_statements(program, stmt->as.if_stmt.then_body,
                             stmt->as.if_stmt.then_count);
      mark_worker_statements(program, stmt->as.if_stmt.else_body,
                             stmt->as.if_stmt.else_count);
      break;
    case ST_WHILE:
      mark_worker_expr(program, stmt->as.while_stmt.condition);
      mark_worker_statements(program, stmt->as.while_stmt.body,
                             stmt->as.while_stmt.count);
      break;
    case ST_RETURN:
      mark_worker_expr(program, stmt->as.return_value);
      break;
    case ST_MATCH:
      mark_worker_expr(program, stmt->as.match_stmt.scrutinee);
      for (size_t arm = 0; arm < stmt->as.match_stmt.arm_count; arm++)
        mark_worker_statements(program, stmt->as.match_stmt.arms[arm].body,
                               stmt->as.match_stmt.arms[arm].body_count);
      break;
    case ST_BREAK:
    case ST_CONTINUE:
      break;
    }
  }
}
static bool expression_calls(Expr *expr, const char *name);
static bool statements_call(Stmt **statements, size_t count, const char *name) {
  for (size_t i = 0; i < count; i++) {
    Stmt *stmt = statements[i];
    switch (stmt->kind) {
    case ST_LET:
      if (expression_calls(stmt->as.let.initializer, name)) return true;
      break;
    case ST_EXPR:
      if (expression_calls(stmt->as.expression, name)) return true;
      break;
    case ST_ASSIGN:
      if (expression_calls(stmt->as.assign.target, name) ||
          expression_calls(stmt->as.assign.value, name)) return true;
      break;
    case ST_IF:
      if (expression_calls(stmt->as.if_stmt.condition, name) ||
          statements_call(stmt->as.if_stmt.then_body, stmt->as.if_stmt.then_count, name) ||
          statements_call(stmt->as.if_stmt.else_body, stmt->as.if_stmt.else_count, name)) return true;
      break;
    case ST_WHILE:
      if (expression_calls(stmt->as.while_stmt.condition, name) ||
          statements_call(stmt->as.while_stmt.body, stmt->as.while_stmt.count, name)) return true;
      break;
    case ST_RETURN:
      if (expression_calls(stmt->as.return_value, name)) return true;
      break;
    case ST_MATCH:
      if (expression_calls(stmt->as.match_stmt.scrutinee, name)) return true;
      for (size_t arm = 0; arm < stmt->as.match_stmt.arm_count; arm++)
        if (statements_call(stmt->as.match_stmt.arms[arm].body,
                            stmt->as.match_stmt.arms[arm].body_count, name)) return true;
      break;
    case ST_BREAK:
    case ST_CONTINUE:
      break;
    }
  }
  return false;
}
static bool expression_calls(Expr *expr, const char *name) {
  if (!expr) return false;
  if (expr->kind == EX_CALL) {
    if (!strcmp(expr->as.call.name, name)) return true;
    for (size_t i = 0; i < expr->as.call.count; i++)
      if (expression_calls(expr->as.call.args[i], name)) return true;
    return false;
  }
  switch (expr->kind) {
  case EX_UNARY: return expression_calls(expr->as.unary.operand, name);
  case EX_BINARY: return expression_calls(expr->as.binary.left, name) ||
                         expression_calls(expr->as.binary.right, name);
  case EX_ARRAY:
    for (size_t i = 0; i < expr->as.array.count; i++)
      if (expression_calls(expr->as.array.items[i], name)) return true;
    return false;
  case EX_INDEX: return expression_calls(expr->as.index.base, name) ||
                         expression_calls(expr->as.index.index, name);
  case EX_FIELD: return expression_calls(expr->as.field.base, name);
  case EX_STRUCT:
    for (size_t i = 0; i < expr->as.structure.count; i++)
      if (expression_calls(expr->as.structure.values[i], name)) return true;
    return false;
  case EX_OPTION: return expression_calls(expr->as.option.value, name);
  case EX_RESULT: return expression_calls(expr->as.result.value, name);
  case EX_INT: case EX_FLOAT: case EX_BOOL: case EX_NIL: case EX_STRING:
  case EX_VAR: case EX_ENUM: case EX_CALL: return false;
  }
  return false;
}
static bool check_program_in_scope(Program *program, Scope *prelude,
                                   Error *error) {
  mark_worker_statements(program, program->statements, program->statement_count);
  for (size_t i = 0; i < program->function_count; i++)
    mark_worker_statements(program, program->functions[i]->body,
                           program->functions[i]->body_count);
  bool worker_changed = true;
  while (worker_changed) {
    worker_changed = false;
    for (size_t i = 0; i < program->function_count; i++) {
      Function *worker = program->functions[i];
      if (!worker->is_worker) continue;
      for (size_t j = 0; j < program->function_count; j++) {
        Function *callee = program->functions[j];
        if (!callee->is_worker && statements_call(worker->body, worker->body_count,
                                                   callee->name)) {
          callee->is_worker = true;
          worker_changed = true;
        }
      }
    }
  }
  for (size_t i = 0; i < program->structure_count && !error->set; i++) {
    StructDecl *decl = program->structures[i];
    for (size_t f = 0; f < decl->field_count; f++) {
      if (!decl->fields[f].resolved) {
        TypeSpec *spec = (TypeSpec *)decl->fields[f].type;
        decl->fields[f].type = resolve_type(program, spec, error);
        decl->fields[f].resolved = true;
      }
      for (size_t prior = 0; prior < f && !error->set; prior++)
        if (!strcmp(decl->fields[prior].name, decl->fields[f].name))
          error_set(error, ERR_TYPE, decl->fields[f].source,
                    decl->fields[f].line, decl->fields[f].column,
                    "duplicate field '%s' in struct '%s'",
                    decl->fields[f].name, decl->name);
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
  for (size_t i = 0; i < program->enumeration_count && !error->set; i++) {
    EnumDecl *decl = program->enumerations[i];
    for (size_t v = 0; v < decl->variant_count && !error->set; v++)
      for (size_t prior = 0; prior < v; prior++)
        if (!strcmp(decl->variants[prior].name, decl->variants[v].name))
          error_set(error, ERR_TYPE, decl->variants[v].source,
                    decl->variants[v].line, decl->variants[v].column,
                    "duplicate variant '%s' in enum '%s'",
                    decl->variants[v].name, decl->name);
  }
  for (size_t i = 0; i < program->function_count && !error->set; i++)
    for (size_t j = i + 1; j < program->function_count; j++)
      if (!strcmp(program->functions[i]->name, program->functions[j]->name))
        error_set(error, ERR_TYPE, program->functions[j]->source,
                  program->functions[j]->line, program->functions[j]->column,
                  "duplicate function '%s'", program->functions[j]->name);
  Scope global = {NULL, prelude, false};
  bool ignored = false;
  check_statements(program, &global, program->statements,
                   program->statement_count, &t_nil, 0, false, error, &ignored);
  for (size_t i = 0; i < program->function_count && !error->set; i++) {
    Function *f = program->functions[i];
    Scope visible_globals = {NULL, NULL, f->is_worker};
    for (Binding *binding = global.bindings; binding; binding = binding->next)
      if (binding->type && binding->type->kind == TYPE_CHANNEL)
        scope_define(&visible_globals, binding->name, binding->type, false,
                     error, f->source, f->line, f->column);
    Scope local = {NULL, &visible_globals, f->is_worker};
    for (size_t p = 0; p < f->parameter_count; p++) {
      Type *type = resolve_type(program, f->parameters[p].type, error);
      scope_define(&local, f->parameters[p].name, type, false, error,
                   f->parameters[p].source, f->parameters[p].line,
                   f->parameters[p].column);
    }
    Type *result = resolve_type(program, f->return_type, error);
    bool returned = false;
    check_statements(program, &local, f->body, f->body_count, result, 0,
                     true, error, &returned);
    if (!error->set && result->kind != TYPE_NIL && result->kind != TYPE_VOID &&
        !returned)
      error_set(error, ERR_TYPE, f->source, f->line, f->column,
                "function '%s' may finish without returning %s", f->name,
                type_label(result));
  }
  if (prelude && !error->set && global.bindings) {
    Binding *tail = global.bindings;
    while (tail->next) tail = tail->next;
    tail->next = prelude->bindings;
    prelude->bindings = global.bindings;
  }
  return !error->set;
}
static bool check_program(Program *program, Error *error) {
  return check_program_in_scope(program, NULL, error);
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
  VAL_RESULT,
  VAL_CHANNEL,
  VAL_THREAD
} ValueKind;
typedef struct Value Value;
typedef struct ChannelRuntime ChannelRuntime;
typedef struct ThreadRuntime ThreadRuntime;
typedef struct Runtime Runtime;
typedef struct RuntimeScope RuntimeScope;
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
    ChannelRuntime *channel;
    ThreadRuntime *thread;
  } as;
};
struct ChannelRuntime {
  pthread_mutex_t mutex;
  pthread_cond_t can_send;
  pthread_cond_t can_receive;
  bool closed;
  Value *queue;
  size_t queue_count;
  size_t queue_capacity;
  size_t queue_head;
  /* Zero means an unbounded channel; nonzero is the maximum queue length. */
  size_t capacity;
  Arena payload_arena;
  pthread_mutex_t payload_mutex;
};
struct ThreadRuntime {
  pthread_t id;
  bool started;
  bool joined;
  Program *program;
  RuntimeScope *channel_scope;
  char *worker_name;
  Error error;
  atomic_bool cancel_requested;
  pthread_mutex_t state_mutex;
  pthread_cond_t state_changed;
  bool finished;
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
  x.as.string.data = aalloc(length + 1);
  if (text && length)
    memcpy(x.as.string.data, text, length);
  x.as.string.data[length] = 0;
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
  case VAL_CHANNEL:
    return a.as.channel == b.as.channel;
  case VAL_THREAD:
    return a.as.thread == b.as.thread;
  }
  return false;
}
typedef struct {
  char *data;
  size_t length;
  size_t capacity;
} Builder;
static void builder_add(Builder *b, const char *text, size_t length) {
  size_t required = 0, need = 0;
  if (!size_add(length, 1, &required) ||
      !size_add(b->length, required, &need))
    fatal_oom();
  if (need > b->capacity) {
    size_t next = 64;
    if (b->capacity) {
      if (b->capacity > SIZE_MAX / 2)
        fatal_oom();
      next = b->capacity * 2;
    }
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
  case VAL_CHANNEL:
    builder_text(b, "<Channel>");
    break;
  case VAL_THREAD:
    builder_text(b, "<Thread>");
    break;
  }
}
static char *value_string(Value v, size_t *length) {
  Builder b = {0};
  stringify(&b, v);
  if (!b.data) {
    if (length)
      *length = 0;
    return astr("");
  }
  if (length)
    *length = b.length;
  return b.data;
}
static bool print_value(FILE *out, Value v) {
  size_t length = 0;
  char *text = value_string(v, &length);
  return fwrite(text, 1, length, out) == length;
}
static size_t utf8_width(unsigned char c) {
  return c < 0x80                 ? 1
         : c >= 0xC2 && c <= 0xDF ? 2
         : c >= 0xE0 && c <= 0xEF ? 3
         : c >= 0xF0 && c <= 0xF4 ? 4
                                  : 0;
}
static bool size_to_int64(size_t value, int64_t *out) {
  if ((uintmax_t)value > (uintmax_t)INT64_MAX)
    return false;
  *out = (int64_t)value;
  return true;
}
static bool decimal_integer_text(const char *text, size_t length) {
  size_t i = 0;
  if (!length)
    return false;
  if (text[i] == '+' || text[i] == '-')
    i++;
  if (i == length)
    return false;
  for (; i < length; i++)
    if (text[i] < '0' || text[i] > '9')
      return false;
  return true;
}
static bool decimal_float_text(const char *text, size_t length) {
  size_t i = 0;
  bool digits_before = false, digits_after = false;
  if (!length)
    return false;
  if (text[i] == '+' || text[i] == '-')
    i++;
  while (i < length && text[i] >= '0' && text[i] <= '9') {
    digits_before = true;
    i++;
  }
  if (i < length && text[i] == '.') {
    i++;
    while (i < length && text[i] >= '0' && text[i] <= '9') {
      digits_after = true;
      i++;
    }
  }
  if (!digits_before && !digits_after)
    return false;
  if (i < length && (text[i] == 'e' || text[i] == 'E')) {
    i++;
    if (i < length && (text[i] == '+' || text[i] == '-'))
      i++;
    size_t exponent_start = i;
    while (i < length && text[i] >= '0' && text[i] <= '9')
      i++;
    if (i == exponent_start)
      return false;
  }
  return i == length;
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
struct RuntimeScope {
  RuntimeBinding *bindings;
  struct RuntimeScope *parent;
};
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
struct Runtime {
  Program *program;
  RuntimeScope *global;
  Error error;
  ChannelRuntime **channels;
  size_t channel_count;
  size_t channel_capacity;
  ThreadRuntime **threads;
  size_t thread_count;
  size_t thread_capacity;
  bool in_worker;
  atomic_bool *cancel_token;
};
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
static bool runtime_cancelled(Runtime *runtime) {
  return runtime && runtime->cancel_token &&
         atomic_load_explicit(runtime->cancel_token, memory_order_acquire);
}
static double numeric_float(Value v) {
  return v.kind == VAL_INT ? (double)v.as.integer : v.as.floating;
}
static Value eval_expr(Runtime *runtime, RuntimeScope *scope, Expr *expr);
static ExecResult execute_statement(Runtime *runtime, RuntimeScope *scope,
                                    Stmt *stmt);

static Value val_channel(ChannelRuntime *channel) {
  Value value = {VAL_CHANNEL, {0}};
  value.as.channel = channel;
  return value;
}
static Value val_thread(ThreadRuntime *thread) {
  Value value = {VAL_THREAD, {0}};
  value.as.thread = thread;
  return value;
}
static Value result_value(bool ok, Value value);
static Value value_clone(Value value) {
  switch (value.kind) {
  case VAL_NIL:
  case VAL_INT:
  case VAL_FLOAT:
  case VAL_BOOL:
  case VAL_ENUM:
    return value;
  case VAL_STRING:
    return val_string_n(value.as.string.data, value.as.string.length);
  case VAL_BYTES:
    return val_bytes_n(value.as.bytes.data, value.as.bytes.length);
  case VAL_ARRAY: {
    size_t bytes = 0;
    if (!size_mul(value.as.array.length, sizeof(Value), &bytes))
      fatal_oom();
    Value copy = {VAL_ARRAY, {0}};
    copy.as.array.length = value.as.array.length;
    copy.as.array.element = value.as.array.element;
    copy.as.array.items = aalloc(bytes ? bytes : 1);
    for (size_t i = 0; i < value.as.array.length; i++)
      copy.as.array.items[i] = value_clone(value.as.array.items[i]);
    return copy;
  }
  case VAL_STRUCT: {
    size_t bytes = 0;
    if (!size_mul(value.as.structure.length, sizeof(Value), &bytes))
      fatal_oom();
    Value copy = {VAL_STRUCT, {0}};
    copy.as.structure.decl = value.as.structure.decl;
    copy.as.structure.length = value.as.structure.length;
    copy.as.structure.fields = aalloc(bytes ? bytes : 1);
    for (size_t i = 0; i < value.as.structure.length; i++)
      copy.as.structure.fields[i] = value_clone(value.as.structure.fields[i]);
    return copy;
  }
  case VAL_OPTION: {
    Value copy = {VAL_OPTION, {0}};
    copy.as.option.present = value.as.option.present;
    if (value.as.option.present) {
      copy.as.option.value = aalloc(sizeof(Value));
      *copy.as.option.value = value_clone(*value.as.option.value);
    }
    return copy;
  }
  case VAL_RESULT: {
    Value copy = {VAL_RESULT, {0}};
    copy.as.result.ok = value.as.result.ok;
    copy.as.result.value = aalloc(sizeof(Value));
    *copy.as.result.value = value_clone(*value.as.result.value);
    return copy;
  }
  case VAL_CHANNEL:
  case VAL_THREAD:
    fatal_oom();
  }
  fatal_oom();
  return val_nil();
}
static Value value_clone_for_channel(ChannelRuntime *channel, Value value) {
  int result = pthread_mutex_lock(&channel->payload_mutex);
  if (result != 0)
    return val_nil();
  Arena *previous = arena_current;
  arena_current = &channel->payload_arena;
  Value copy = value_clone(value);
  arena_current = previous;
  pthread_mutex_unlock(&channel->payload_mutex);
  return copy;
}
static bool channel_queue_grow(ChannelRuntime *channel, size_t need) {
  if (need <= channel->queue_capacity)
    return true;
  size_t next = channel->queue_capacity ? channel->queue_capacity * 2 : 8;
  while (next < need) {
    if (next > SIZE_MAX / 2)
      return false;
    next *= 2;
  }
  size_t bytes = 0;
  if (!size_mul(next, sizeof(Value), &bytes))
    return false;
  Arena *previous = arena_current;
  arena_current = &channel->payload_arena;
  Value *grown = aalloc(bytes);
  for (size_t i = 0; i < channel->queue_count; i++)
    grown[i] = channel->queue[(channel->queue_head + i) %
                               channel->queue_capacity];
  arena_current = previous;
  channel->queue = grown;
  channel->queue_capacity = next;
  channel->queue_head = 0;
  return true;
}
static bool channel_queue_push(ChannelRuntime *channel, Value value) {
  size_t need = 0;
  if (!size_add(channel->queue_count, 1, &need) ||
      !channel_queue_grow(channel, need))
    return false;
  size_t tail = (channel->queue_head + channel->queue_count) %
                channel->queue_capacity;
  channel->queue[tail] = value;
  channel->queue_count++;
  return true;
}
static bool runtime_add_channel(Runtime *runtime, ChannelRuntime *channel) {
  runtime->channels = agrow(runtime->channels, runtime->channel_count,
                             &runtime->channel_capacity,
                             runtime->channel_count + 1, sizeof(ChannelRuntime *));
  runtime->channels[runtime->channel_count++] = channel;
  return true;
}
static bool runtime_add_thread(Runtime *runtime, ThreadRuntime *thread) {
  runtime->threads = agrow(runtime->threads, runtime->thread_count,
                            &runtime->thread_capacity,
                            runtime->thread_count + 1, sizeof(ThreadRuntime *));
  runtime->threads[runtime->thread_count++] = thread;
  return true;
}
static ChannelRuntime *runtime_channel_make(Runtime *runtime, Expr *expr,
                                             size_t capacity) {
  ChannelRuntime *channel = aalloc(sizeof(*channel));
  int result = pthread_mutex_init(&channel->mutex, NULL);
  if (result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot initialize channel mutex: %s",
              strerror(result));
    return NULL;
  }
  result = pthread_cond_init(&channel->can_send, NULL);
  if (result != 0) {
    pthread_mutex_destroy(&channel->mutex);
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot initialize channel condition: %s",
              strerror(result));
    return NULL;
  }
  result = pthread_cond_init(&channel->can_receive, NULL);
  if (result != 0) {
    pthread_cond_destroy(&channel->can_send);
    pthread_mutex_destroy(&channel->mutex);
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot initialize channel condition: %s",
              strerror(result));
    return NULL;
  }
  result = pthread_mutex_init(&channel->payload_mutex, NULL);
  if (result != 0) {
    pthread_cond_destroy(&channel->can_receive);
    pthread_cond_destroy(&channel->can_send);
    pthread_mutex_destroy(&channel->mutex);
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot initialize channel payload mutex: %s",
              strerror(result));
    return NULL;
  }
  channel->capacity = capacity;
  runtime_add_channel(runtime, channel);
  return channel;
}
static bool runtime_channel_close(Runtime *runtime, ChannelRuntime *channel,
                                  Expr *expr) {
  int result = pthread_mutex_lock(&channel->mutex);
  if (result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot lock channel: %s", strerror(result));
    return false;
  }
  channel->closed = true;
  pthread_cond_broadcast(&channel->can_send);
  pthread_cond_broadcast(&channel->can_receive);
  result = pthread_mutex_unlock(&channel->mutex);
  if (result != 0)
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot unlock channel: %s", strerror(result));
  return result == 0;
}
static bool runtime_channel_send(Runtime *runtime, ChannelRuntime *channel,
                                 Value value, Expr *expr) {
  int result = pthread_mutex_lock(&channel->mutex);
  if (result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot lock channel: %s", strerror(result));
    return false;
  }
  while (channel->capacity && channel->queue_count >= channel->capacity &&
         !channel->closed && !runtime_cancelled(runtime)) {
    result = pthread_cond_wait(&channel->can_send, &channel->mutex);
    if (result != 0) {
      pthread_mutex_unlock(&channel->mutex);
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "cannot wait to send on channel: %s",
                strerror(result));
      return false;
    }
  }
  if (runtime_cancelled(runtime)) {
    pthread_mutex_unlock(&channel->mutex);
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "channel send cancelled");
    return false;
  }
  if (channel->closed) {
    pthread_mutex_unlock(&channel->mutex);
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot send on a closed channel");
    return false;
  }
  Value copy = value_clone_for_channel(channel, value);
  if (!channel_queue_push(channel, copy)) {
    pthread_mutex_unlock(&channel->mutex);
    error_set(&runtime->error, ERR_RESOURCE, expr->source, expr->line,
              expr->column, "channel queue allocation overflow");
    return false;
  }
  pthread_cond_signal(&channel->can_receive);
  result = pthread_mutex_unlock(&channel->mutex);
  if (result != 0)
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot unlock channel: %s", strerror(result));
  return result == 0;
}
static Value runtime_channel_receive(Runtime *runtime, ChannelRuntime *channel,
                                     Expr *expr) {
  int result = pthread_mutex_lock(&channel->mutex);
  if (result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot lock channel: %s", strerror(result));
    return val_nil();
  }
  while (!channel->queue_count && !channel->closed &&
         !runtime_cancelled(runtime)) {
    result = pthread_cond_wait(&channel->can_receive, &channel->mutex);
    if (result != 0) {
      pthread_mutex_unlock(&channel->mutex);
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "cannot wait to receive from channel: %s",
                strerror(result));
      return val_nil();
    }
  }
  if (runtime_cancelled(runtime)) {
    pthread_mutex_unlock(&channel->mutex);
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "channel receive cancelled");
    return val_nil();
  }
  if (!channel->queue_count) {
    pthread_mutex_unlock(&channel->mutex);
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot receive from a closed channel");
    return val_nil();
  }
  Value value = channel->queue[channel->queue_head];
  channel->queue_head = (channel->queue_head + 1) % channel->queue_capacity;
  channel->queue_count--;
  pthread_cond_signal(&channel->can_send);
  result = pthread_mutex_unlock(&channel->mutex);
  if (result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot unlock channel: %s", strerror(result));
    return val_nil();
  }
  return value;
}
static bool channel_deadline(int64_t milliseconds, struct timespec *deadline,
                             Error *error, Expr *expr) {
  if (milliseconds < 0) {
    error_set(error, ERR_RUNTIME, expr->source, expr->line, expr->column,
              "channel timeout duration cannot be negative");
    return false;
  }
  if (clock_gettime(CLOCK_REALTIME, deadline) != 0) {
    error_set(error, ERR_RUNTIME, expr->source, expr->line, expr->column,
              "cannot read the system clock: %s", strerror(errno));
    return false;
  }
  int64_t seconds = milliseconds / 1000;
  long nanoseconds = (long)((milliseconds % 1000) * 1000000);
  if (seconds > INT64_MAX - (int64_t)deadline->tv_sec) {
    error_set(error, ERR_RUNTIME, expr->source, expr->line, expr->column,
              "channel timeout deadline overflow");
    return false;
  }
  deadline->tv_sec += (time_t)seconds;
  deadline->tv_nsec += nanoseconds;
  if (deadline->tv_nsec >= 1000000000L) {
    deadline->tv_sec++;
    deadline->tv_nsec -= 1000000000L;
  }
  return true;
}
static Value channel_receive_locked(Runtime *runtime, ChannelRuntime *channel,
                                    Expr *expr) {
  if (!channel->queue_count) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot receive from a closed channel");
    return val_nil();
  }
  Value value = channel->queue[channel->queue_head];
  channel->queue_head = (channel->queue_head + 1) % channel->queue_capacity;
  channel->queue_count--;
  pthread_cond_signal(&channel->can_send);
  return value;
}
static Value runtime_channel_receive_timeout(Runtime *runtime,
                                             ChannelRuntime *channel,
                                             int64_t milliseconds, Expr *expr) {
  struct timespec deadline;
  if (!channel_deadline(milliseconds, &deadline, &runtime->error, expr))
    return val_nil();
  int result = pthread_mutex_lock(&channel->mutex);
  if (result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot lock channel: %s", strerror(result));
    return val_nil();
  }
  while (!channel->queue_count && !channel->closed &&
         !runtime_cancelled(runtime)) {
    result = pthread_cond_timedwait(&channel->can_receive, &channel->mutex,
                                    &deadline);
    if (result == ETIMEDOUT) {
      pthread_mutex_unlock(&channel->mutex);
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "channel receive timed out");
      return val_nil();
    }
    if (result != 0) {
      pthread_mutex_unlock(&channel->mutex);
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "cannot wait to receive from channel: %s",
                strerror(result));
      return val_nil();
    }
  }
  if (runtime_cancelled(runtime)) {
    pthread_mutex_unlock(&channel->mutex);
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "channel receive cancelled");
    return val_nil();
  }
  if (!channel->queue_count) {
    pthread_mutex_unlock(&channel->mutex);
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot receive from a closed channel");
    return val_nil();
  }
  Value value = channel_receive_locked(runtime, channel, expr);
  result = pthread_mutex_unlock(&channel->mutex);
  if (result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot unlock channel: %s", strerror(result));
    return val_nil();
  }
  return value;
}
static Value runtime_channel_try_receive(Runtime *runtime,
                                         ChannelRuntime *channel, Expr *expr) {
  int result = pthread_mutex_lock(&channel->mutex);
  if (result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot lock channel: %s", strerror(result));
    return val_nil();
  }
  if (!channel->queue_count) {
    bool closed = channel->closed;
    pthread_mutex_unlock(&channel->mutex);
    return result_value(false, val_string_n(closed ? "closed" : "empty",
                                            closed ? 6 : 5));
  }
  Value value = channel_receive_locked(runtime, channel, expr);
  pthread_mutex_unlock(&channel->mutex);
  return result_value(true, value);
}
static bool runtime_cleanup(Runtime *runtime);
static void *thread_entry(void *argument) {
  ThreadRuntime *thread = argument;
  Arena arena = {0};
  Arena *previous = arena_current;
  arena_current = &arena;
  Runtime worker = {0};
  worker.program = thread->program;
  worker.in_worker = true;
  worker.cancel_token = &thread->cancel_requested;
  worker.global = runtime_scope(thread->channel_scope);
  Function *function = find_function(worker.program, thread->worker_name);
  if (!function) {
    error_set(&worker.error, ERR_RUNTIME, NULL, 1, 1,
              "unknown thread worker '%s'", thread->worker_name);
  } else {
    for (size_t i = 0; i < function->body_count; i++) {
      ExecResult result = execute_statement(&worker, worker.global,
                                            function->body[i]);
      if (result.code == EXEC_RETURN)
        break;
      if (result.code == EXEC_BREAK || result.code == EXEC_CONTINUE) {
        error_set(&worker.error, ERR_RUNTIME, function->body[i]->source,
                  function->body[i]->line, function->body[i]->column,
                  "loop control is only valid inside a while loop");
        break;
      }
      if (result.code == EXEC_ERROR || worker.error.set)
        break;
    }
  }
  runtime_cleanup(&worker);
  if (worker.error.set)
    thread->error = worker.error;
  int state_result = pthread_mutex_lock(&thread->state_mutex);
  if (state_result == 0) {
    thread->finished = true;
    pthread_cond_broadcast(&thread->state_changed);
    pthread_mutex_unlock(&thread->state_mutex);
  }
  arena_free(&arena);
  arena_current = previous;
  return NULL;
}
static ThreadRuntime *runtime_thread_spawn(Runtime *runtime, Expr *expr,
                                           const char *worker_name,
                                           size_t worker_length) {
  if (runtime->in_worker) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "worker threads cannot spawn nested threads");
    return NULL;
  }
  if (memchr(worker_name, '\0', worker_length) != NULL) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "thread worker name cannot contain NUL");
    return NULL;
  }
  Function *function = find_function(runtime->program, worker_name);
  if (!function || function->parameter_count != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "thread worker '%s' must take no arguments",
              worker_name);
    return NULL;
  }
  ThreadRuntime *thread = aalloc(sizeof(*thread));
  thread->program = runtime->program;
  atomic_init(&thread->cancel_requested, false);
  int state_result = pthread_mutex_init(&thread->state_mutex, NULL);
  if (state_result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot initialize thread state: %s",
              strerror(state_result));
    return NULL;
  }
  state_result = pthread_cond_init(&thread->state_changed, NULL);
  if (state_result != 0) {
    pthread_mutex_destroy(&thread->state_mutex);
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot initialize thread state condition: %s",
              strerror(state_result));
    return NULL;
  }
  thread->program = runtime->program;
  thread->channel_scope = runtime_scope(NULL);
  for (RuntimeBinding *binding = runtime->global->bindings; binding;
       binding = binding->next)
    if (binding->value.kind == VAL_CHANNEL)
      runtime_define(thread->channel_scope, binding->name, binding->value, false);
  thread->worker_name = astr(worker_name);
  runtime_add_thread(runtime, thread);
  int result = pthread_create(&thread->id, NULL, thread_entry, thread);
  if (result != 0) {
    pthread_cond_destroy(&thread->state_changed);
    pthread_mutex_destroy(&thread->state_mutex);
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot start thread: %s", strerror(result));
    return NULL;
  }
  thread->started = true;
  return thread;
}
static bool runtime_thread_join(Runtime *runtime, ThreadRuntime *thread,
                                Expr *expr) {
  if (!thread || !thread->started) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot join a thread that did not start");
    return false;
  }
  if (thread->joined)
    return true;
  if (pthread_equal(pthread_self(), thread->id)) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "a thread cannot join itself");
    return false;
  }
  int result = pthread_join(thread->id, NULL);
  if (result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot join thread: %s", strerror(result));
    return false;
  }
  thread->joined = true;
  if (thread->error.set) {
    runtime->error = thread->error;
    return false;
  }
  return true;
}
static bool runtime_cleanup(Runtime *runtime) {
  for (size_t i = 0; i < runtime->thread_count; i++)
    atomic_store_explicit(&runtime->threads[i]->cancel_requested, true,
                          memory_order_release);
  for (size_t i = 0; i < runtime->channel_count; i++) {
    ChannelRuntime *channel = runtime->channels[i];
    int result = pthread_mutex_lock(&channel->mutex);
    if (result == 0) {
      channel->closed = true;
      pthread_cond_broadcast(&channel->can_send);
      pthread_cond_broadcast(&channel->can_receive);
      pthread_mutex_unlock(&channel->mutex);
    } else if (!runtime->error.set) {
      error_set(&runtime->error, ERR_RUNTIME, NULL, 1, 1,
                "cannot close channel during shutdown: %s", strerror(result));
    }
  }
  for (size_t i = 0; i < runtime->thread_count; i++)
    if (runtime->threads[i]->started && !runtime->threads[i]->joined) {
      int result = pthread_join(runtime->threads[i]->id, NULL);
      if (result != 0 && !runtime->error.set)
        error_set(&runtime->error, ERR_RUNTIME, NULL, 1, 1,
                  "cannot join thread during shutdown: %s", strerror(result));
      else
        runtime->threads[i]->joined = true;
      if (runtime->threads[i]->error.set && !runtime->error.set)
        runtime->error = runtime->threads[i]->error;
    }
  for (size_t i = 0; i < runtime->channel_count; i++) {
    ChannelRuntime *channel = runtime->channels[i];
    pthread_cond_destroy(&channel->can_send);
    pthread_cond_destroy(&channel->can_receive);
    pthread_mutex_destroy(&channel->mutex);
    pthread_mutex_destroy(&channel->payload_mutex);
    arena_free(&channel->payload_arena);
  }
  for (size_t i = 0; i < runtime->thread_count; i++) {
    pthread_cond_destroy(&runtime->threads[i]->state_changed);
    pthread_mutex_destroy(&runtime->threads[i]->state_mutex);
  }
  return !runtime->error.set;
}
static char *read_file(const char *path, size_t *length, Error *error);
static bool sandbox_prefix(const char *path, const char *root) {
  size_t length = strlen(root);
  return !strncmp(path, root, length) &&
         (path[length] == 0 || path[length] == '/');
}
static bool unsafe_path_text(const char *path) {
  if (!path || path[0] == '/') return true;
  const char *start = path;
  for (const char *p = path;; p++) {
    if (*p == '/' || *p == 0) {
      size_t length = (size_t)(p - start);
      if (length == 2 && start[0] == '.' && start[1] == '.') return true;
      if (!*p) break;
      start = p + 1;
    }
  }
  return false;
}
static char *sandbox_resolve(const char *path, bool must_exist) {
  if (!restricted_mode) return NULL;
  if (unsafe_path_text(path)) return NULL;
  size_t root_length = strlen(sandbox_root), path_length = strlen(path), total = 0;
  if (!size_add(root_length, 1, &total) || !size_add(total, path_length + 1, &total)) return NULL;
  char *candidate = malloc(total);
  if (!candidate) return NULL;
  snprintf(candidate, total, "%s/%s", sandbox_root, path);
  char resolved[PATH_MAX];
  if (must_exist) {
    if (!realpath(candidate, resolved) || !sandbox_prefix(resolved, sandbox_root)) { free(candidate); return NULL; }
    free(candidate); return astr(resolved);
  }
  char *slash = strrchr(candidate, '/');
  if (!slash) { free(candidate); return NULL; }
  *slash = 0;
  if (!realpath(candidate, resolved) || !sandbox_prefix(resolved, sandbox_root)) { free(candidate); return NULL; }
  size_t parent_length = strlen(resolved), base_length = strlen(slash + 1), needed = 0;
  if (!size_add(parent_length, 1, &needed) || !size_add(needed, base_length + 1, &needed)) { free(candidate); return NULL; }
  char *result = aalloc(needed);
  snprintf(result, needed, "%s/%s", resolved, slash + 1);
  free(candidate);
  return result;
}
static Value result_value(bool ok, Value value) {
  Value *inner = aalloc(sizeof(Value));
  *inner = value;
  Value result = {VAL_RESULT, {0}};
  result.as.result.ok = ok;
  result.as.result.value = inner;
  return result;
}
static Value runtime_fs_read_text(Runtime *runtime, Expr *expr, Value path) {
  if (memchr(path.as.string.data, '\0', path.as.string.length))
    return result_value(false, val_string_n("path contains NUL", 17));
  const char *resolved = path.as.string.data;
  if (restricted_mode && !(resolved = sandbox_resolve(path.as.string.data, true)))
    return result_value(false, val_string_n("path denied by sandbox", 22));
  Error read_error = {0};
  size_t length = 0;
  char *data = read_file(resolved, &length, &read_error);
  if (!data)
    return result_value(false, val_string_n(read_error.message,
                                            strlen(read_error.message)));
  if (!utf8_valid((const unsigned char *)data, length)) {
    free(data);
    return result_value(false, val_string_n("file is not valid UTF-8", 23));
  }
  Value text = val_string_n(data, length);
  free(data);
  (void)runtime;
  (void)expr;
  return result_value(true, text);
}
static Value runtime_fs_write_text(Runtime *runtime, Expr *expr, Value path,
                                   Value text) {
  if (memchr(path.as.string.data, '\0', path.as.string.length))
    return result_value(false, val_string_n("path contains NUL", 17));
  const char *resolved = path.as.string.data;
  if (restricted_mode && !(resolved = sandbox_resolve(path.as.string.data, false)))
    return result_value(false, val_string_n("path denied by sandbox", 22));
  FILE *file = fopen(resolved, "wb");
  if (!file) {
    const char *message = strerror(errno);
    return result_value(false, val_string_n(message, strlen(message)));
  }
  bool ok = fwrite(text.as.string.data, 1, text.as.string.length, file) ==
            text.as.string.length;
  if (fclose(file) != 0)
    ok = false;
  if (!ok)
    return result_value(false, val_string_n("failed to write text file", 25));
  (void)runtime;
  (void)expr;
  return result_value(true, val_nil());
}
static Value runtime_env_get(Runtime *runtime, Expr *expr, Value name) {
  if (memchr(name.as.string.data, '\0', name.as.string.length)) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "environment name contains NUL");
    return val_nil();
  }
  const char *value = getenv(name.as.string.data);
  if (!value)
    return (Value){VAL_OPTION, {0}};
  if (!utf8_valid((const unsigned char *)value, strlen(value))) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "environment value is not valid UTF-8");
    return val_nil();
  }
  Value result = {VAL_OPTION, {0}};
  result.as.option.present = true;
  result.as.option.value = aalloc(sizeof(Value));
  *result.as.option.value = val_string_n(value, strlen(value));
  return result;
}
static Value runtime_failure(const char *message) {
  return result_value(false, val_string_n(message, strlen(message)));
}
static Value option_value(bool present, Value value) {
  Value result = {VAL_OPTION, {0}};
  result.as.option.present = present;
  if (present) {
    result.as.option.value = aalloc(sizeof(Value));
    *result.as.option.value = value;
  }
  return result;
}
static Value array_make(size_t length, Type *element) {
  size_t bytes = 0;
  if (!size_mul(length, sizeof(Value), &bytes))
    return val_nil();
  Value result = {VAL_ARRAY, {0}};
  result.as.array.length = length;
  result.as.array.element = element;
  result.as.array.items = aalloc(bytes ? bytes : 1);
  return result;
}
static const char *find_bytes(const char *text, size_t text_length,
                              const char *needle, size_t needle_length) {
  if (!needle_length)
    return text;
  if (needle_length > text_length)
    return NULL;
  for (size_t i = 0; i <= text_length - needle_length; i++)
    if (!memcmp(text + i, needle, needle_length))
      return text + i;
  return NULL;
}
static bool string_codepoint_offset(const char *text, size_t length,
                                    int64_t index, size_t *offset) {
  if (index < 0)
    return false;
  size_t current = 0;
  for (int64_t i = 0; i < index; i++) {
    if (current >= length)
      return false;
    size_t width = utf8_width((unsigned char)text[current]);
    if (!width || width > length - current)
      return false;
    current += width;
  }
  *offset = current;
  return true;
}
static Value runtime_substring(Value text, Value start, Value length) {
  if (!utf8_valid((const unsigned char *)text.as.string.data,
                  text.as.string.length))
    return runtime_failure("invalid UTF-8 String");
  size_t begin = 0, end = 0;
  if (!string_codepoint_offset(text.as.string.data, text.as.string.length,
                               start.as.integer, &begin) ||
      !string_codepoint_offset(text.as.string.data, text.as.string.length,
                               start.as.integer + length.as.integer, &end) ||
      length.as.integer < 0)
    return runtime_failure("substring range is out of bounds");
  return result_value(true, val_string_n(text.as.string.data + begin,
                                         end - begin));
}
static Value runtime_trim(Value text) {
  size_t begin = 0, end = text.as.string.length;
  while (begin < end && strchr(" \t\r\n", text.as.string.data[begin]))
    begin++;
  while (end > begin && strchr(" \t\r\n", text.as.string.data[end - 1]))
    end--;
  return val_string_n(text.as.string.data + begin, end - begin);
}
static Value runtime_split(Value text, Value separator) {
  Value result = array_make(0, &t_string);
  size_t position = 0;
  if (!separator.as.string.length) {
    result = array_make(1, &t_string);
    result.as.array.items[0] = val_string_n(text.as.string.data, text.as.string.length);
    return result;
  }
  for (;;) {
    const char *found = find_bytes(text.as.string.data + position,
                                   text.as.string.length - position,
                                   separator.as.string.data,
                                   separator.as.string.length);
    size_t end = found ? (size_t)(found - text.as.string.data) : text.as.string.length;
    Value part = val_string_n(text.as.string.data + position, end - position);
    size_t next_length = 0;
    if (!size_add(result.as.array.length, 1, &next_length))
      return val_nil();
    Value grown = array_make(next_length, &t_string);
    for (size_t i = 0; i < result.as.array.length; i++)
      grown.as.array.items[i] = result.as.array.items[i];
    grown.as.array.items[next_length - 1] = part;
    result = grown;
    if (!found)
      break;
    position = end + separator.as.string.length;
  }
  return result;
}
static Value runtime_replace(Value text, Value from, Value to) {
  if (!from.as.string.length)
    return val_string_n(text.as.string.data, text.as.string.length);
  size_t occurrences = 0, position = 0;
  while (position <= text.as.string.length) {
    const char *found = find_bytes(text.as.string.data + position,
                                   text.as.string.length - position,
                                   from.as.string.data, from.as.string.length);
    if (!found)
      break;
    occurrences++;
    position = (size_t)(found - text.as.string.data) + from.as.string.length;
  }
  size_t removed = 0, added = 0, length = text.as.string.length;
  if (!size_mul(occurrences, from.as.string.length, &removed) ||
      !size_mul(occurrences, to.as.string.length, &added) ||
      removed > length || !size_add(length - removed, added, &length))
    return val_nil();
  Value result = val_string_n(NULL, length);
  position = 0;
  size_t output = 0;
  while (position < text.as.string.length) {
    const char *found = find_bytes(text.as.string.data + position,
                                   text.as.string.length - position,
                                   from.as.string.data, from.as.string.length);
    size_t end = found ? (size_t)(found - text.as.string.data) : text.as.string.length;
    size_t part = end - position;
    memcpy(result.as.string.data + output, text.as.string.data + position, part);
    output += part;
    position = end;
    if (found) {
      memcpy(result.as.string.data + output, to.as.string.data,
             to.as.string.length);
      output += to.as.string.length;
      position += from.as.string.length;
    }
  }
  return result;
}
static Value runtime_codepoints(Value text) {
  if (!utf8_valid((const unsigned char *)text.as.string.data,
                  text.as.string.length))
    return val_nil();
  size_t count = utf8_count(text.as.string.data, text.as.string.length);
  Value result = array_make(count, &t_int);
  size_t offset = 0, index = 0;
  while (offset < text.as.string.length) {
    unsigned char c = (unsigned char)text.as.string.data[offset];
    uint32_t codepoint = 0;
    size_t width = utf8_width(c);
    if (width == 1)
      codepoint = c;
    else if (width == 2)
      codepoint = (uint32_t)(c & 0x1f) << 6 |
                  (uint32_t)((unsigned char)text.as.string.data[offset + 1] & 0x3f);
    else if (width == 3)
      codepoint = (uint32_t)(c & 0x0f) << 12 |
                  (uint32_t)((unsigned char)text.as.string.data[offset + 1] & 0x3f) << 6 |
                  (uint32_t)((unsigned char)text.as.string.data[offset + 2] & 0x3f);
    else
      codepoint = (uint32_t)(c & 0x07) << 18 |
                  (uint32_t)((unsigned char)text.as.string.data[offset + 1] & 0x3f) << 12 |
                  (uint32_t)((unsigned char)text.as.string.data[offset + 2] & 0x3f) << 6 |
                  (uint32_t)((unsigned char)text.as.string.data[offset + 3] & 0x3f);
    result.as.array.items[index++] = val_int((int64_t)codepoint);
    offset += width;
  }
  return result;
}
static Value runtime_hex_encode(Value bytes) {
  static const char digits[] = "0123456789abcdef";
  size_t length = 0;
  if (!size_mul(bytes.as.bytes.length, 2, &length))
    return val_nil();
  Value result = val_string_n(NULL, length);
  for (size_t i = 0; i < bytes.as.bytes.length; i++) {
    result.as.string.data[i * 2] = digits[bytes.as.bytes.data[i] >> 4];
    result.as.string.data[i * 2 + 1] = digits[bytes.as.bytes.data[i] & 15];
  }
  return result;
}
static int hex_digit(unsigned char c) {
  if (c >= '0' && c <= '9')
    return c - '0';
  if (c >= 'a' && c <= 'f')
    return c - 'a' + 10;
  if (c >= 'A' && c <= 'F')
    return c - 'A' + 10;
  return -1;
}
static Value runtime_hex_decode(Value text) {
  if (text.as.string.length % 2)
    return runtime_failure("hex text must have even length");
  size_t length = text.as.string.length / 2;
  unsigned char *data = aalloc(length ? length : 1);
  for (size_t i = 0; i < length; i++) {
    int high = hex_digit((unsigned char)text.as.string.data[i * 2]);
    int low = hex_digit((unsigned char)text.as.string.data[i * 2 + 1]);
    if (high < 0 || low < 0)
      return runtime_failure("hex text contains a non-hex digit");
    data[i] = (unsigned char)(high * 16 + low);
  }
  return result_value(true, val_bytes_n(data, length));
}
static Value runtime_base64_encode(Value bytes) {
  static const char alphabet[] =
      "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
  size_t groups = (bytes.as.bytes.length + 2) / 3, length = 0;
  if (!size_mul(groups, 4, &length))
    return val_nil();
  Value result = val_string_n(NULL, length);
  size_t in = 0, out = 0;
  while (in < bytes.as.bytes.length) {
    size_t start = in;
    uint32_t a = bytes.as.bytes.data[in++];
    uint32_t b = in < bytes.as.bytes.length ? bytes.as.bytes.data[in++] : 0;
    uint32_t c = in < bytes.as.bytes.length ? bytes.as.bytes.data[in++] : 0;
    size_t remaining = bytes.as.bytes.length - start;
    result.as.string.data[out++] = alphabet[(a >> 2) & 63];
    result.as.string.data[out++] = alphabet[((a & 3) << 4) | (b >> 4)];
    result.as.string.data[out++] = remaining > 1 ? alphabet[((b & 15) << 2) | (c >> 6)] : '=';
    result.as.string.data[out++] = remaining > 2 ? alphabet[c & 63] : '=';
  }
  return result;
}
static int base64_digit(unsigned char c) {
  if (c >= 'A' && c <= 'Z') return c - 'A';
  if (c >= 'a' && c <= 'z') return c - 'a' + 26;
  if (c >= '0' && c <= '9') return c - '0' + 52;
  if (c == '+') return 62;
  if (c == '/') return 63;
  return -1;
}
static Value runtime_base64_decode(Value text) {
  if (text.as.string.length % 4)
    return runtime_failure("base64 text length must be a multiple of four");
  size_t groups = text.as.string.length / 4, length = 0;
  if (groups && !size_mul(groups, 3, &length))
    return runtime_failure("base64 text is too large");
  unsigned char *data = aalloc(length ? length : 1);
  size_t out = 0;
  for (size_t i = 0; i < groups; i++) {
    const unsigned char *p = (const unsigned char *)text.as.string.data + i * 4;
    int a = base64_digit(p[0]), b = base64_digit(p[1]);
    int c = p[2] == '=' ? 0 : base64_digit(p[2]);
    int d = p[3] == '=' ? 0 : base64_digit(p[3]);
    bool last = i + 1 == groups;
    if (a < 0 || b < 0 || c < 0 || d < 0 || (!last && (p[2] == '=' || p[3] == '=')) ||
        (p[2] == '=' && p[3] != '='))
      return runtime_failure("malformed base64 text");
    data[out++] = (unsigned char)((a << 2) | (b >> 4));
    if (p[2] != '=') data[out++] = (unsigned char)(((b & 15) << 4) | (c >> 2));
    if (p[3] != '=') data[out++] = (unsigned char)(((c & 3) << 6) | d);
  }
  return result_value(true, val_bytes_n(data, out));
}
static bool float_to_int(double value, int64_t *out) {
  if (!isfinite(value) || value < -(double)INT64_MAX - 1.0 ||
      value >= (double)INT64_MAX + 1.0)
    return false;
  *out = (int64_t)value;
  return true;
}
static Value runtime_channel_send_result(Runtime *runtime, ChannelRuntime *channel,
                                              Value value, int64_t milliseconds,
                                              bool timed, bool nonblocking,
                                              Expr *expr) {
  struct timespec deadline;
  if (timed && !channel_deadline(milliseconds, &deadline, &runtime->error, expr))
    return val_nil();
  int result = pthread_mutex_lock(&channel->mutex);
  if (result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot lock channel: %s", strerror(result));
    return val_nil();
  }
  while (channel->capacity && channel->queue_count >= channel->capacity &&
         !channel->closed && !runtime_cancelled(runtime)) {
    if (nonblocking) {
      pthread_mutex_unlock(&channel->mutex);
      return runtime_failure("full");
    }
    if (!timed)
      result = pthread_cond_wait(&channel->can_send, &channel->mutex);
    else
      result = pthread_cond_timedwait(&channel->can_send, &channel->mutex,
                                      &deadline);
    if (result == ETIMEDOUT) {
      pthread_mutex_unlock(&channel->mutex);
      return runtime_failure("timeout");
    }
    if (result != 0) {
      pthread_mutex_unlock(&channel->mutex);
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "cannot wait to send on channel: %s",
                strerror(result));
      return val_nil();
    }
  }
  if (runtime_cancelled(runtime)) {
    pthread_mutex_unlock(&channel->mutex);
    return runtime_failure("cancelled");
  }
  if (channel->closed) {
    pthread_mutex_unlock(&channel->mutex);
    return runtime_failure("closed");
  }
  Value copy = value_clone_for_channel(channel, value);
  if (!channel_queue_push(channel, copy)) {
    pthread_mutex_unlock(&channel->mutex);
    return runtime_failure("resource");
  }
  pthread_cond_signal(&channel->can_receive);
  pthread_mutex_unlock(&channel->mutex);
  return result_value(true, val_nil());
}
static Value runtime_thread_join_result(Runtime *runtime, ThreadRuntime *thread,
                                        int64_t milliseconds, bool timed,
                                        Expr *expr) {
  if (!thread || !thread->started)
    return runtime_failure("not-started");
  if (pthread_equal(pthread_self(), thread->id))
    return runtime_failure("self-join");
  if (thread->joined)
    return thread->error.set ? runtime_failure(thread->error.message)
                             : result_value(true, val_nil());
  int result = pthread_mutex_lock(&thread->state_mutex);
  if (result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot lock thread state: %s", strerror(result));
    return val_nil();
  }
  if (!thread->finished && timed) {
    struct timespec deadline;
    if (!channel_deadline(milliseconds, &deadline, &runtime->error, expr)) {
      pthread_mutex_unlock(&thread->state_mutex);
      return val_nil();
    }
    while (!thread->finished) {
      result = pthread_cond_timedwait(&thread->state_changed,
                                      &thread->state_mutex, &deadline);
      if (result == ETIMEDOUT) {
        pthread_mutex_unlock(&thread->state_mutex);
        return runtime_failure("timeout");
      }
      if (result != 0) {
        pthread_mutex_unlock(&thread->state_mutex);
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "cannot wait for thread: %s", strerror(result));
        return val_nil();
      }
    }
  } else if (!thread->finished) {
    while (!thread->finished) {
      result = pthread_cond_wait(&thread->state_changed, &thread->state_mutex);
      if (result != 0) {
        pthread_mutex_unlock(&thread->state_mutex);
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "cannot wait for thread: %s", strerror(result));
        return val_nil();
      }
    }
  }
  pthread_mutex_unlock(&thread->state_mutex);
  result = pthread_join(thread->id, NULL);
  if (result != 0) {
    error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
              expr->column, "cannot join thread: %s", strerror(result));
    return val_nil();
  }
  thread->joined = true;
  if (thread->error.set)
    return runtime_failure(thread->error.message);
  return result_value(true, val_nil());
}
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
    if (!print_value(stdout, args[0]) || fflush(stdout) != 0)
      error_set(&runtime->error, ERR_IO, expr->source, expr->line, expr->column,
                "failed to write standard output");
    return val_nil();
  case B_PRINTLN:
    if (!print_value(stdout, args[0]) || fputc('\n', stdout) == EOF ||
        fflush(stdout) != 0)
      error_set(&runtime->error, ERR_IO, expr->source, expr->line, expr->column,
                "failed to write standard output");
    return val_nil();
  case B_LEN:
    if (args[0].kind == VAL_STRING) {
      if (!utf8_valid((const unsigned char *)args[0].as.string.data,
                      args[0].as.string.length)) {
        error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                  expr->column, "len received invalid UTF-8 String");
        return val_nil();
      }
      int64_t length_as_int = 0;
      if (!size_to_int64(utf8_count(args[0].as.string.data,
                                    args[0].as.string.length), &length_as_int)) {
        error_set(&runtime->error, ERR_RESOURCE, expr->source, expr->line,
                  expr->column, "String length exceeds the Int range");
        return val_nil();
      }
      return val_int(length_as_int);
    }
    if (args[0].kind == VAL_ARRAY) {
      int64_t length_as_int = 0;
      if (!size_to_int64(args[0].as.array.length, &length_as_int)) {
        error_set(&runtime->error, ERR_RESOURCE, expr->source, expr->line,
                  expr->column, "Array length exceeds the Int range");
        return val_nil();
      }
      return val_int(length_as_int);
    }
    if (args[0].kind == VAL_BYTES) {
      int64_t length_as_int = 0;
      if (!size_to_int64(args[0].as.bytes.length, &length_as_int)) {
        error_set(&runtime->error, ERR_RESOURCE, expr->source, expr->line,
                  expr->column, "Bytes length exceeds the Int range");
        return val_nil();
      }
      return val_int(length_as_int);
    }
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
      size_t consumed = end && end >= args[0].as.string.data
                            ? (size_t)(end - args[0].as.string.data)
                            : 0;
      if (!decimal_integer_text(args[0].as.string.data,
                                args[0].as.string.length) ||
          errno == ERANGE || end == args[0].as.string.data ||
          consumed != args[0].as.string.length ||
          value > (intmax_t)INT64_MAX || value < (intmax_t)INT64_MIN) {
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
      size_t consumed = end && end >= args[0].as.string.data
                            ? (size_t)(end - args[0].as.string.data)
                            : 0;
      if (!decimal_float_text(args[0].as.string.data,
                              args[0].as.string.length) ||
          errno == ERANGE || end == args[0].as.string.data ||
          consumed != args[0].as.string.length || !isfinite(value)) {
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
    size_t length = 0;
    char *text = value_string(args[0], &length);
    return val_string_n(text, length);
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
  case B_MIN:
    if (args[0].kind == VAL_INT)
      return val_int(args[0].as.integer < args[1].as.integer ? args[0].as.integer
                                                              : args[1].as.integer);
    return val_float(args[0].as.floating < args[1].as.floating ? args[0].as.floating
                                                                 : args[1].as.floating);
  case B_MAX:
    if (args[0].kind == VAL_INT)
      return val_int(args[0].as.integer > args[1].as.integer ? args[0].as.integer
                                                              : args[1].as.integer);
    return val_float(args[0].as.floating > args[1].as.floating ? args[0].as.floating
                                                                 : args[1].as.floating);
  case B_FLOOR:
  case B_CEIL:
  case B_ROUND: {
    double rounded = spec->id == B_FLOOR ? floor(args[0].as.floating)
                   : spec->id == B_CEIL ? ceil(args[0].as.floating)
                                        : round(args[0].as.floating);
    int64_t value = 0;
    if (!float_to_int(rounded, &value)) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "math result is outside the Int range");
      return val_nil();
    }
    return val_int(value);
  }
  case B_POW:
  case B_LOG:
  case B_SIN:
  case B_COS: {
    double result = spec->id == B_POW ? pow(args[0].as.floating, args[1].as.floating)
                    : spec->id == B_LOG ? log(args[0].as.floating)
                    : spec->id == B_SIN ? sin(args[0].as.floating)
                                        : cos(args[0].as.floating);
    if (!isfinite(result))
      return runtime_failure("non-finite math result");
    return val_float(result);
  }
  case B_IS_NAN:
    return val_bool(isnan(args[0].as.floating));
  case B_IS_FINITE:
    return val_bool(isfinite(args[0].as.floating));
  case B_IS_SOME:
    return val_bool(args[0].kind == VAL_OPTION && args[0].as.option.present);
  case B_IS_NONE:
    return val_bool(args[0].kind == VAL_OPTION && !args[0].as.option.present);
  case B_IS_OK:
    return val_bool(args[0].kind == VAL_RESULT && args[0].as.result.ok);
  case B_IS_ERR:
    return val_bool(args[0].kind == VAL_RESULT && !args[0].as.result.ok);
  case B_UNWRAP_OR:
    if (args[0].kind == VAL_OPTION && args[0].as.option.present)
      return *args[0].as.option.value;
    return args[1];
  case B_SUBSTRING:
    return runtime_substring(args[0], args[1], args[2]);
  case B_CONTAINS:
    return val_bool(find_bytes(args[0].as.string.data, args[0].as.string.length,
                               args[1].as.string.data, args[1].as.string.length) != NULL);
  case B_STARTS_WITH:
    return val_bool(args[1].as.string.length <= args[0].as.string.length &&
                    !memcmp(args[0].as.string.data, args[1].as.string.data,
                            args[1].as.string.length));
  case B_ENDS_WITH:
    return val_bool(args[1].as.string.length <= args[0].as.string.length &&
                    !memcmp(args[0].as.string.data + args[0].as.string.length -
                                args[1].as.string.length,
                            args[1].as.string.data, args[1].as.string.length));
  case B_TRIM:
    return runtime_trim(args[0]);
  case B_SPLIT:
    return runtime_split(args[0], args[1]);
  case B_REPLACE:
    return runtime_replace(args[0], args[1], args[2]);
  case B_CODEPOINTS:
    return runtime_codepoints(args[0]);
  case B_BYTE_AT:
    if (args[1].as.integer < 0 || (uintmax_t)args[1].as.integer >= args[0].as.string.length)
      return runtime_failure("byte index out of bounds");
    return result_value(true, val_int((unsigned char)args[0].as.string.data[args[1].as.integer]));
  case B_HEX_ENCODE:
    return runtime_hex_encode(args[0]);
  case B_HEX_DECODE:
    return runtime_hex_decode(args[0]);
  case B_BASE64_ENCODE:
    return runtime_base64_encode(args[0]);
  case B_BASE64_DECODE:
    return runtime_base64_decode(args[0]);
  case B_ARRAY_POP:
    if (!args[0].as.array.length)
      return option_value(false, val_nil());
    return option_value(true, args[0].as.array.items[args[0].as.array.length - 1]);
  case B_ARRAY_GET:
    if (args[1].as.integer < 0 || (uintmax_t)args[1].as.integer >= args[0].as.array.length)
      return option_value(false, val_nil());
    return option_value(true, args[0].as.array.items[args[1].as.integer]);
  case B_ARRAY_CONCAT: {
    size_t length = 0;
    if (!size_add(args[0].as.array.length, args[1].as.array.length, &length))
      return runtime_failure("array size overflow");
    Value result = array_make(length, args[0].as.array.element);
    for (size_t i = 0; i < args[0].as.array.length; i++)
      result.as.array.items[i] = args[0].as.array.items[i];
    for (size_t i = 0; i < args[1].as.array.length; i++)
      result.as.array.items[args[0].as.array.length + i] = args[1].as.array.items[i];
    return result;
  }
  case B_ARRAY_CONTAINS:
    for (size_t i = 0; i < args[0].as.array.length; i++)
      if (value_equal(args[0].as.array.items[i], args[1]))
        return val_bool(true);
    return val_bool(false);
  case B_ARRAY_SLICE: {
    if (args[1].as.integer < 0 || args[2].as.integer < 0 ||
        (uintmax_t)args[1].as.integer > args[0].as.array.length ||
        (uintmax_t)args[2].as.integer > args[0].as.array.length - (size_t)args[1].as.integer)
      return val_nil();
    size_t start = (size_t)args[1].as.integer, length = (size_t)args[2].as.integer;
    Value result = array_make(length, args[0].as.array.element);
    for (size_t i = 0; i < length; i++)
      result.as.array.items[i] = args[0].as.array.items[start + i];
    return result;
  }
  case B_ARRAY_REVERSE: {
    Value result = array_make(args[0].as.array.length, args[0].as.array.element);
    for (size_t i = 0; i < args[0].as.array.length; i++)
      result.as.array.items[i] = args[0].as.array.items[args[0].as.array.length - i - 1];
    return result;
  }
  case B_ARRAY_JOIN: {
    Value result = val_string_n(NULL, 0);
    for (size_t i = 0; i < args[0].as.array.length; i++) {
      size_t old = result.as.string.length, add = args[0].as.array.items[i].as.string.length;
      if (i && !size_add(add, args[1].as.string.length, &add)) return val_nil();
      size_t total = 0;
      if (!size_add(old, add, &total)) return val_nil();
      Value next = val_string_n(NULL, total);
      size_t out = 0;
      memcpy(next.as.string.data, result.as.string.data, old); out += old;
      if (i) { memcpy(next.as.string.data + out, args[1].as.string.data, args[1].as.string.length); out += args[1].as.string.length; }
      memcpy(next.as.string.data + out, args[0].as.array.items[i].as.string.data,
             args[0].as.array.items[i].as.string.length);
      result = next;
    }
    return result;
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
  case B_THREAD_CHANNEL: {
    ChannelRuntime *channel = runtime_channel_make(runtime, expr, 0);
    return runtime->error.set ? val_nil() : val_channel(channel);
  }
  case B_THREAD_CHANNEL_WITH_CAPACITY: {
    if (args[0].as.integer <= 0 || (uintmax_t)args[0].as.integer > SIZE_MAX)
      return runtime_failure("channel capacity must be positive");
    ChannelRuntime *channel = runtime_channel_make(runtime, expr,
                                                   (size_t)args[0].as.integer);
    return runtime->error.set ? val_nil() : val_channel(channel);
  }
  case B_THREAD_SPAWN: {
    ThreadRuntime *thread = runtime_thread_spawn(
        runtime, expr, args[0].as.string.data, args[0].as.string.length);
    return runtime->error.set ? val_nil() : val_thread(thread);
  }
  case B_THREAD_SEND:
    if (args[0].kind != VAL_CHANNEL) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "thread_send expects a Channel");
      return val_nil();
    }
    runtime_channel_send(runtime, args[0].as.channel, args[1], expr);
    return val_nil();
  case B_THREAD_TRY_SEND:
    if (args[0].kind != VAL_CHANNEL)
      return runtime_failure("not-channel");
    return runtime_channel_send_result(runtime, args[0].as.channel, args[1], 0,
                                       false, true, expr);
  case B_THREAD_SEND_TIMEOUT:
    if (args[0].kind != VAL_CHANNEL || args[2].kind != VAL_INT)
      return runtime_failure("invalid-arguments");
    return runtime_channel_send_result(runtime, args[0].as.channel, args[1],
                                       args[2].as.integer, true, false, expr);
  case B_THREAD_RECEIVE:
    if (args[0].kind != VAL_CHANNEL) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "thread_receive expects a Channel");
      return val_nil();
    }
    return runtime_channel_receive(runtime, args[0].as.channel, expr);
  case B_THREAD_TRY_RECEIVE:
    if (args[0].kind != VAL_CHANNEL)
      return runtime_failure("not-channel");
    return runtime_channel_try_receive(runtime, args[0].as.channel, expr);
  case B_THREAD_RECEIVE_TIMEOUT:
    if (args[0].kind != VAL_CHANNEL || args[1].kind != VAL_INT) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column,
                "thread_receive_timeout expects Channel[T] and Int milliseconds");
      return val_nil();
    }
    return runtime_channel_receive_timeout(runtime, args[0].as.channel,
                                           args[1].as.integer, expr);
  case B_THREAD_JOIN:
    if (args[0].kind != VAL_THREAD) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "thread_join expects a Thread");
      return val_nil();
    }
    runtime_thread_join(runtime, args[0].as.thread, expr);
    return val_nil();
  case B_THREAD_JOIN_TIMEOUT:
    if (args[0].kind != VAL_THREAD || args[1].kind != VAL_INT)
      return runtime_failure("invalid-arguments");
    return runtime_thread_join_result(runtime, args[0].as.thread,
                                      args[1].as.integer, true, expr);
  case B_THREAD_CANCEL:
    if (args[0].kind != VAL_THREAD)
      return val_nil();
    atomic_store_explicit(&args[0].as.thread->cancel_requested, true,
                          memory_order_release);
    for (size_t i = 0; i < runtime->channel_count; i++) {
      pthread_mutex_lock(&runtime->channels[i]->mutex);
      pthread_cond_broadcast(&runtime->channels[i]->can_send);
      pthread_cond_broadcast(&runtime->channels[i]->can_receive);
      pthread_mutex_unlock(&runtime->channels[i]->mutex);
    }
    return val_nil();
  case B_THREAD_CLOSE:
    if (args[0].kind != VAL_CHANNEL) {
      error_set(&runtime->error, ERR_RUNTIME, expr->source, expr->line,
                expr->column, "thread_close expects a Channel");
      return val_nil();
    }
    runtime_channel_close(runtime, args[0].as.channel, expr);
    return val_nil();
  case B_FS_READ_TEXT:
    return runtime_fs_read_text(runtime, expr, args[0]);
  case B_FS_WRITE_TEXT:
    return runtime_fs_write_text(runtime, expr, args[0], args[1]);
  case B_FS_READ_BYTES: {
    if (memchr(args[0].as.string.data, '\0', args[0].as.string.length))
      return result_value(false, val_string_n("path contains NUL", 17));
    const char *resolved = args[0].as.string.data;
    if (restricted_mode && !(resolved = sandbox_resolve(args[0].as.string.data, true)))
      return result_value(false, val_string_n("path denied by sandbox", 22));
    Error read_error = {0};
    size_t length = 0;
    char *data = read_file(resolved, &length, &read_error);
    if (!data)
      return result_value(false, val_string_n(read_error.message,
                                              strlen(read_error.message)));
    Value result = result_value(true, val_bytes_n((unsigned char *)data, length));
    free(data);
    return result;
  }
  case B_FS_WRITE_BYTES: {
    if (memchr(args[0].as.string.data, '\0', args[0].as.string.length))
      return result_value(false, val_string_n("path contains NUL", 17));
    const char *resolved = args[0].as.string.data;
    if (restricted_mode && !(resolved = sandbox_resolve(args[0].as.string.data, false)))
      return result_value(false, val_string_n("path denied by sandbox", 22));
    FILE *file = fopen(resolved, "wb");
    if (!file) {
      const char *message = strerror(errno);
      return result_value(false, val_string_n(message, strlen(message)));
    }
    bool ok = fwrite(args[1].as.bytes.data, 1, args[1].as.bytes.length, file) ==
              args[1].as.bytes.length;
    if (fclose(file) != 0)
      ok = false;
    return ok ? result_value(true, val_nil())
              : result_value(false, val_string_n("failed to write bytes file", 26));
  }
  case B_FS_EXISTS:
    if (memchr(args[0].as.string.data, '\0', args[0].as.string.length))
      return val_bool(false);
    if (restricted_mode) {
      const char *resolved = sandbox_resolve(args[0].as.string.data, true);
      return val_bool(resolved != NULL);
    }
    return val_bool(access(args[0].as.string.data, F_OK) == 0);
  case B_ENV_GET:
    return runtime_env_get(runtime, expr, args[0]);
  case B_COUNT:
    return val_nil();
  }
  return val_nil();
}

static RuntimeScope *runtime_visible_globals(Runtime *runtime) {
  RuntimeScope *visible = runtime_scope(NULL);
  for (RuntimeBinding *binding = runtime->global->bindings; binding;
       binding = binding->next)
    if (binding->value.kind == VAL_CHANNEL)
      runtime_define(visible, binding->name, binding->value, false);
  return visible;
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
  RuntimeScope *local = runtime_scope(runtime_visible_globals(runtime));
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
    return val_string_n(expr->as.string, expr->string_length);
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
      if (left.kind == VAL_INT) {
        int64_t a = left.as.integer, b = right.as.integer;
        return val_bool(op == TOK_LESS         ? a < b
                        : op == TOK_LESS_EQUAL ? a <= b
                        : op == TOK_GREATER    ? a > b
                                               : a >= b);
      }
      double a = left.as.floating, b = right.as.floating;
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
    size_t field_bytes = 0;
    if (!size_mul(decl->field_count, sizeof(Value), &field_bytes)) {
      error_set(&runtime->error, ERR_RESOURCE, expr->source, expr->line,
                expr->column, "struct allocation size overflow");
      return val_nil();
    }
    result.as.structure.fields = aalloc(field_bytes ? field_bytes : 1);
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
           value.as.string.length == pattern->text_length &&
           !memcmp(value.as.string.data, pattern->text, pattern->text_length);
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
  Runtime runtime = {0};
  runtime.program = program;
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
  runtime_cleanup(&runtime);
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
  size_t configured_limit = max_source_bytes > max_artifact_bytes
                                 ? max_source_bytes : max_artifact_bytes;
  if ((uintmax_t)end > (uintmax_t)SIZE_MAX - 1 ||
      (uintmax_t)end > (uintmax_t)configured_limit) {
    fclose(file);
    error_set(error, ERR_RESOURCE, NULL, 1, 1,
              "file '%s' exceeds the configured input size limit", path);
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
typedef struct {
  char *logical_path;
  char *data;
  size_t length;
} BundleEntry;
typedef struct {
  uint32_t state[8];
  uint64_t bits;
  unsigned char block[64];
  size_t used;
} Sha256;
static uint32_t sha_rotr(uint32_t value, unsigned int amount) {
  return (value >> amount) | (value << (32U - amount));
}
static void sha256_transform(Sha256 *sha, const unsigned char *block) {
  static const uint32_t constants[64] = {
      0x428a2f98U, 0x71374491U, 0xb5c0fbcfU, 0xe9b5dba5U, 0x3956c25bU,
      0x59f111f1U, 0x923f82a4U, 0xab1c5ed5U, 0xd807aa98U, 0x12835b01U,
      0x243185beU, 0x550c7dc3U, 0x72be5d74U, 0x80deb1feU, 0x9bdc06a7U,
      0xc19bf174U, 0xe49b69c1U, 0xefbe4786U, 0x0fc19dc6U, 0x240ca1ccU,
      0x2de92c6fU, 0x4a7484aaU, 0x5cb0a9dcU, 0x76f988daU, 0x983e5152U,
      0xa831c66dU, 0xb00327c8U, 0xbf597fc7U, 0xc6e00bf3U, 0xd5a79147U,
      0x06ca6351U, 0x14292967U, 0x27b70a85U, 0x2e1b2138U, 0x4d2c6dfcU,
      0x53380d13U, 0x650a7354U, 0x766a0abbU, 0x81c2c92eU, 0x92722c85U,
      0xa2bfe8a1U, 0xa81a664bU, 0xc24b8b70U, 0xc76c51a3U, 0xd192e819U,
      0xd6990624U, 0xf40e3585U, 0x106aa070U, 0x19a4c116U, 0x1e376c08U,
      0x2748774cU, 0x34b0bcb5U, 0x391c0cb3U, 0x4ed8aa4aU, 0x5b9cca4fU,
      0x682e6ff3U, 0x748f82eeU, 0x78a5636fU, 0x84c87814U, 0x8cc70208U,
      0x90befffaU, 0xa4506cebU, 0xbef9a3f7U, 0xc67178f2U};
  uint32_t words[64];
  for (size_t i = 0; i < 16; i++)
    words[i] = ((uint32_t)block[i * 4] << 24) |
              ((uint32_t)block[i * 4 + 1] << 16) |
              ((uint32_t)block[i * 4 + 2] << 8) | block[i * 4 + 3];
  for (size_t i = 16; i < 64; i++) {
    uint32_t s0 = sha_rotr(words[i - 15], 7) ^ sha_rotr(words[i - 15], 18) ^
                  (words[i - 15] >> 3);
    uint32_t s1 = sha_rotr(words[i - 2], 17) ^ sha_rotr(words[i - 2], 19) ^
                  (words[i - 2] >> 10);
    words[i] = words[i - 16] + s0 + words[i - 7] + s1;
  }
  uint32_t a = sha->state[0], b = sha->state[1], c = sha->state[2],
           d = sha->state[3], e = sha->state[4], f = sha->state[5],
           g = sha->state[6], h = sha->state[7];
  for (size_t i = 0; i < 64; i++) {
    uint32_t s1 = sha_rotr(e, 6) ^ sha_rotr(e, 11) ^ sha_rotr(e, 25);
    uint32_t choice = (e & f) ^ ((~e) & g);
    uint32_t temp1 = h + s1 + choice + constants[i] + words[i];
    uint32_t s0 = sha_rotr(a, 2) ^ sha_rotr(a, 13) ^ sha_rotr(a, 22);
    uint32_t majority = (a & b) ^ (a & c) ^ (b & c);
    uint32_t temp2 = s0 + majority;
    h = g; g = f; f = e; e = d + temp1; d = c; c = b; b = a; a = temp1 + temp2;
  }
  sha->state[0] += a; sha->state[1] += b; sha->state[2] += c; sha->state[3] += d;
  sha->state[4] += e; sha->state[5] += f; sha->state[6] += g; sha->state[7] += h;
}
static void sha256_init(Sha256 *sha) {
  static const uint32_t initial[8] = {0x6a09e667U, 0xbb67ae85U, 0x3c6ef372U,
      0xa54ff53aU, 0x510e527fU, 0x9b05688cU, 0x1f83d9abU, 0x5be0cd19U};
  memcpy(sha->state, initial, sizeof(initial));
  sha->bits = 0; sha->used = 0;
}
static void sha256_update(Sha256 *sha, const unsigned char *data, size_t length) {
  if (length > UINT64_MAX / 8 - sha->bits)
    fatal_oom();
  sha->bits += (uint64_t)length * 8;
  while (length) {
    size_t chunk = 64 - sha->used;
    if (chunk > length) chunk = length;
    memcpy(sha->block + sha->used, data, chunk);
    sha->used += chunk; data += chunk; length -= chunk;
    if (sha->used == 64) { sha256_transform(sha, sha->block); sha->used = 0; }
  }
}
static void sha256_final(Sha256 *sha, unsigned char output[32]) {
  size_t used = sha->used;
  sha->block[used++] = 0x80;
  if (used > 56) { memset(sha->block + used, 0, 64 - used); sha256_transform(sha, sha->block); used = 0; }
  memset(sha->block + used, 0, 56 - used);
  for (int i = 0; i < 8; i++) sha->block[56 + i] = (unsigned char)(sha->bits >> (56 - 8 * i));
  sha256_transform(sha, sha->block);
  for (size_t i = 0; i < 8; i++) {
    output[i * 4] = (unsigned char)(sha->state[i] >> 24);
    output[i * 4 + 1] = (unsigned char)(sha->state[i] >> 16);
    output[i * 4 + 2] = (unsigned char)(sha->state[i] >> 8);
    output[i * 4 + 3] = (unsigned char)sha->state[i];
  }
}
static const char artifact_magic[] = "KRYNATIVE2\n";
static const uint32_t artifact_format_version = 2;
static const char artifact_compiler_version[] = KRY_VERSION;
static const char artifact_target[] = "posix-c11";
static bool is_artifact(const unsigned char *data, size_t length) {
  size_t magic_length = sizeof(artifact_magic) - 1;
  if (length >= magic_length && !memcmp(data, artifact_magic, magic_length))
    return true;
  return length == magic_length - 1 && !memcmp(data, artifact_magic, length);
}
static uint32_t artifact_u32(const unsigned char *data) {
  return (uint32_t)data[0] | ((uint32_t)data[1] << 8) |
         ((uint32_t)data[2] << 16) | ((uint32_t)data[3] << 24);
}
static uint64_t artifact_u64(const unsigned char *data) {
  uint64_t value = 0;
  for (size_t i = 0; i < 8; i++)
    value |= (uint64_t)data[i] << (8 * i);
  return value;
}
static void artifact_put_u32(FILE *file, uint32_t value, bool *ok) {
  unsigned char data[4] = {(unsigned char)value, (unsigned char)(value >> 8),
                           (unsigned char)(value >> 16), (unsigned char)(value >> 24)};
  if (*ok && fwrite(data, 1, sizeof(data), file) != sizeof(data)) *ok = false;
}
static void artifact_put_u64(FILE *file, uint64_t value, bool *ok) {
  unsigned char data[8];
  for (size_t i = 0; i < 8; i++) data[i] = (unsigned char)(value >> (8 * i));
  if (*ok && fwrite(data, 1, sizeof(data), file) != sizeof(data)) *ok = false;
}
#include "kry_artifacts.inc"

typedef struct ModuleLoader {
  char **paths;
  size_t count;
  size_t capacity;
  char **stack;
  size_t stack_count;
  size_t stack_capacity;
  char *root_dir;
  BundleEntry *bundle;
  size_t bundle_count;
  Error *error;
} ModuleLoader;
static BundleEntry *bundle_find(ModuleLoader *loader, const char *logical_path) {
  for (size_t i = 0; i < loader->bundle_count; i++)
    if (!strcmp(loader->bundle[i].logical_path, logical_path))
      return &loader->bundle[i];
  return NULL;
}
static const char *bundle_logical_path(ModuleLoader *loader, const char *path) {
  size_t root_length = strlen(loader->root_dir);
  if (!strncmp(path, loader->root_dir, root_length) &&
      (path[root_length] == '/' || path[root_length] == 0))
    return path[root_length] == '/' ? path + root_length + 1 : path + root_length;
  return path;
}
static bool path_has_kry_suffix(const char *path) {
  size_t length = strlen(path);
  return length >= 4 && !memcmp(path + length - 4, ".kry", 4);
}
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
  size_t a = strlen(directory), b = strlen(relative), separator = 0,
         total = 0;
  if (!size_add(b, 2, &separator) || !size_add(a, separator, &total))
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
  for (size_t i = 0; i < module->function_count && !loader->error->set; i++) {
    for (size_t j = 0; j < target->function_count; j++)
      if (!strcmp(target->functions[j]->name, module->functions[i]->name)) {
        error_set(loader->error, ERR_TYPE, module->functions[i]->source,
                  module->functions[i]->line, module->functions[i]->column,
                  "duplicate imported function '%s'",
                  module->functions[i]->name);
        break;
      }
    if (loader->error->set) break;
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
  char *data = NULL;
  bool data_owned = false;
  size_t length = 0;
  const char *source_name = NULL;
  if (loader->bundle_count) {
    const char *logical = bundle_logical_path(loader, path);
    BundleEntry *entry = bundle_find(loader, logical);
    if (!entry) {
      error_set(loader->error, ERR_ARTIFACT, NULL, 1, 1,
                "artifact dependency '%s' is not embedded", logical);
      return NULL;
    }
    if (snprintf(resolved, sizeof(resolved), "%s", path) >= (int)sizeof(resolved)) {
      error_set(loader->error, ERR_RESOURCE, NULL, 1, 1,
                "embedded module path is too long");
      return NULL;
    }
    data = entry->data;
    length = entry->length;
    source_name = entry->logical_path;
  } else {
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
    source_name = resolved;
    Error read_error = {0};
    data = read_file(resolved, &length, &read_error);
    data_owned = true;
    if (!data) {
      *loader->error = read_error;
      return NULL;
    }
  }
  if (loader_on_stack(loader, resolved)) {
    if (data_owned) free(data);
    error_set(loader->error, ERR_TYPE, NULL, 1, 1,
              "cyclic module import involving '%s'", source_name);
    return NULL;
  }
  if (loader_seen(loader, resolved)) {
    if (data_owned) free(data);
    return target;
  }
  loader_add(loader, resolved);
  loader->stack =
      agrow(loader->stack, loader->stack_count, &loader->stack_capacity,
            loader->stack_count + 1, sizeof(char *));
  loader->stack[loader->stack_count++] = astr(resolved);
  if (is_artifact((unsigned char *)data, length)) {
    if (data_owned) free(data);
    error_set(loader->error, ERR_ARTIFACT, NULL, 1, 1,
              "modules must import source files, not native artifacts");
    return NULL;
  }
  Source *source = source_make(source_name, data, length);
  if (data_owned) free(data);
  program_add_source(target, source);
  Error parse_error = {0};
  Program *module = parse_program(source, &parse_error);
  if (!module) {
    *loader->error = parse_error;
    return NULL;
  }
  for (size_t i = 0; i < module->import_count && !loader->error->set; i++) {
    char *relative = module->imports[i].path;
    if (memchr(relative, '\0', module->imports[i].path_length) ||
        relative[0] == '/' || path_has_parent(relative)) {
      error_set(loader->error, ERR_IO, module->imports[i].source,
                module->imports[i].line, module->imports[i].column,
                "unsafe module path '%s'", relative);
      break;
    }
    char *with_extension = astr(relative);
    if (!path_has_kry_suffix(relative)) {
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
                          size_t source_length, BundleEntry *bundle,
                          size_t bundle_count, Error *error) {
  Source *root_source = source_make(path, source_text, source_length);
  Program *program = parse_program(root_source, error);
  if (!program)
    return NULL;
  program_add_source(program, root_source);
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
  loader.bundle = bundle;
  loader.bundle_count = bundle_count;
  loader.root_dir = astr(resolved);
  char *slash = strrchr(loader.root_dir, '/');
  if (slash)
    *slash = 0;
  for (size_t i = 0; i < program->import_count && !error->set; i++) {
    char *relative = program->imports[i].path;
    if (memchr(relative, '\0', program->imports[i].path_length) ||
        relative[0] == '/' || path_has_parent(relative)) {
      error_set(error, ERR_IO, program->imports[i].source,
                program->imports[i].line, program->imports[i].column,
                "unsafe module path '%s'", relative);
      break;
    }
    char *with_extension = astr(relative);
    if (!path_has_kry_suffix(relative)) {
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
  Program *program = load_root(path, data, length, NULL, 0, &error);
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
static bool build_artifact_source(const char *path, const char *data,
                                  size_t length, const char *output,
                                  Error *error) {
  Arena arena = {0};
  Arena *previous = arena_current;
  arena_current = &arena;
  Program *program = load_root(path, data, length, NULL, 0, error);
  bool ok = program && check_program(program, error) &&
            write_artifact(output, program, error);
  arena_free(&arena);
  arena_current = previous;
  return ok;
}
static bool same_existing_path(const char *left, const char *right) {
  if (!strcmp(left, right)) return true;
  char a[PATH_MAX], b[PATH_MAX];
  return realpath(left, a) && realpath(right, b) && !strcmp(a, b);
}
static char *default_output(const char *input) {
  const char *dot = strrchr(input, '.');
  size_t length = dot ? (size_t)(dot - input) : strlen(input);
  size_t allocation = 0;
  if (!size_add(length, 6, &allocation))
    return NULL;
  char *output = malloc(allocation);
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
typedef struct {
  Arena arena;
  Scope type_scope;
  Runtime runtime;
  Function **functions;
  size_t function_count;
  size_t function_capacity;
  StructDecl **structures;
  size_t structure_count;
  size_t structure_capacity;
  EnumDecl **enumerations;
  size_t enumeration_count;
  size_t enumeration_capacity;
  bool initialized;
} ReplSession;
static size_t delimiter_balance(const char *data, size_t length) {
  size_t balance = 0;
  bool quoted = false, escaped = false;
  for (size_t i = 0; i < length; i++) {
    unsigned char c = (unsigned char)data[i];
    if (quoted) {
      if (escaped) escaped = false;
      else if (c == '\\') escaped = true;
      else if (c == '"') quoted = false;
      continue;
    }
    if (c == '"') quoted = true;
    else if (c == '{') balance++;
    else if (c == '}' && balance) balance--;
  }
  return balance;
}
static int process_repl_line(const char *data, size_t length, ReplSession *session);
static int run_repl(void) {
  char *line = NULL;
  size_t line_capacity = 0;
  bool interactive = isatty(STDIN_FILENO);
  ReplSession session = {0};
  if (interactive)
    printf("Kryndel REPL %s. Enter :help for help or :quit to exit.\n", KRY_VERSION);
  for (;;) {
    if (interactive) { fputs("kry> ", stdout); fflush(stdout); }
    char *buffer = NULL;
    size_t used = 0, capacity = 0;
    for (;;) {
      if (interactive) { fputs(used ? "...> " : "kry> ", stdout); fflush(stdout); }
      ssize_t got = getline(&line, &line_capacity, stdin);
      if (got < 0) { free(buffer); free(line); goto done; }
      size_t chunk = (size_t)got;
      if (chunk && line[chunk - 1] == '\n') chunk--;
      size_t needed = 0;
      if (!size_add(used, chunk + (used ? 1 : 0), &needed)) { free(buffer); free(line); return 2; }
      if (needed + 1 > capacity) {
        size_t next = capacity ? capacity * 2 : 128;
        while (next < needed + 1) next *= 2;
        char *grown = realloc(buffer, next);
        if (!grown) { free(buffer); free(line); return 2; }
        buffer = grown; capacity = next;
      }
      if (used) buffer[used++] = '\n';
      memcpy(buffer + used, line, chunk); used += chunk; buffer[used] = 0;
      if (!delimiter_balance(buffer, used)) break;
    }
    if (!used) { free(buffer); continue; }
    if (buffer[0] == ':') {
      if (!strcmp(buffer, ":quit") || !strcmp(buffer, ":q")) { free(buffer); break; }
      if (!strcmp(buffer, ":help")) { puts(":help  Show help\n:quit  Exit\n:reset  Clear state\n:type  Check an expression\n:load FILE  Load source\n:version  Print version"); free(buffer); continue; }
      if (!strcmp(buffer, ":version")) { printf("Kryndel %s\n", KRY_VERSION); free(buffer); continue; }
      if (!strcmp(buffer, ":reset")) {
        if (session.initialized) runtime_cleanup(&session.runtime);
        arena_free(&session.arena);
        session = (ReplSession){0};
        puts("repl: reset"); free(buffer); continue;
      }
      if (!strncmp(buffer, ":type ", 6)) { process_repl_line(buffer + 6, strlen(buffer + 6), &session); free(buffer); continue; }
      if (!strncmp(buffer, ":load ", 6)) {
        Error error = {0}; size_t length = 0; char *data = read_file(buffer + 6, &length, &error);
        if (!data) print_error(&error); else process_repl_line(data, length, &session);
        free(data); free(buffer); continue;
      }
      fprintf(stderr, "error[cli]: unknown REPL command\n"); free(buffer); continue;
    }
    process_repl_line(buffer, used, &session);
    free(buffer);
  }
done:
  free(line);
  if (session.initialized) runtime_cleanup(&session.runtime);
  arena_free(&session.arena);
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
  bool tree_ok = source_ok && access("Makefile", R_OK) == 0 &&
                 access("examples/hello.kry", R_OK) == 0 &&
                 access("docs", R_OK) == 0;
  bool build_ok = access("build", W_OK) == 0 || access(".", W_OK) == 0;
  bool registry_ok = builtin_registry_valid();
  const char *configured_cc = getenv("CC");
  bool compiler_ok = configured_cc && *configured_cc
                         ? compiler_available(configured_cc)
                         : compiler_available("cc") || compiler_available("gcc") ||
                               compiler_available("clang");
  printf("Kryndel doctor\n");
  printf("native compiler: %s\n", compiler_ok ? "available" : "unavailable");
  printf("required libraries: math and pthreads linked\n");
  printf("builtin registry: %s\n", registry_ok ? "complete" : "incomplete");
  printf("native source: %s\n", source_ok ? "available" : "missing");
  printf("source tree: %s\n", tree_ok ? "available" : "incomplete");
  printf("output directory: %s\n", build_ok ? "writable" : "not writable");
  printf("locale: %s\n",
         getenv("LC_ALL") || getenv("LANG") ? "configured" : "C default");
  printf("supported features: %s\n",
         tree_ok ? "static checking, artifacts, UTF-8, threads, typed file I/O"
                 : "unavailable");
  if (!compiler_ok || !tree_ok || !build_ok || !registry_ok) {
    puts("doctor: issues found");
    return 1;
  }
  puts("doctor: ready");
  return 0;
}
static void usage(void) {
  printf("Kryndel %s — strict native language toolchain\n", KRY_VERSION);
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
  puts("  kry --json <command>             Render diagnostics as JSON");
  puts("  kry --restricted ROOT <command>  Restrict filesystem APIs to ROOT");
  puts("  kry --max-source BYTES <command> Reject oversized source input");
  puts("  kry --max-artifact BYTES <command> Reject oversized artifacts");
  puts("  kry --help                       Show this help");
  puts("\nSource syntax uses let/let mut, typed functions, Bool conditions, "
       "Array[T], modules, structs, enums, and match.");
  puts("Builtins:");
  for (size_t i = 0; i < builtin_count; i++)
    printf("  %-52s %s\n", builtins[i].signature, builtins[i].description);
}
static bool parse_limit(const char *text, size_t *result) {
  if (!text || !*text || text[0] == '-') return false;
  errno = 0;
  char *end = NULL;
  uintmax_t value = strtoumax(text, &end, 10);
  if (errno == ERANGE || !end || *end || value > SIZE_MAX) return false;
  *result = (size_t)value;
  return true;
}
static int cli_error(const char *message) {
  if (json_diagnostics) {
    Error error = {0};
    error_set(&error, ERR_CLI, NULL, 1, 1, "%s", message);
    print_error(&error);
  } else {
    fprintf(stderr, "error[cli]: %s\n", message);
  }
  return 2;
}
int main(int argc, char **argv) {
  (void)setlocale(LC_ALL, "C");
  while (argc >= 2) {
    if (!strcmp(argv[1], "--json")) {
      json_diagnostics = true;
      argv++; argc--; continue;
    }
    if (!strcmp(argv[1], "--restricted")) {
      if (argc < 3) return cli_error("--restricted requires a sandbox root");
      char resolved[PATH_MAX];
      if (!realpath(argv[2], resolved) || access(resolved, R_OK | X_OK) != 0)
        return cli_error("--restricted root must be an accessible directory");
      sandbox_root = strdup(resolved);
      if (!sandbox_root) return 2;
      restricted_mode = true;
      argv += 2; argc -= 2; continue;
    }
    if (!strcmp(argv[1], "--max-source")) {
      if (argc < 3 || !parse_limit(argv[2], &max_source_bytes))
        return cli_error("--max-source requires a non-negative byte limit");
      argv += 2; argc -= 2; continue;
    }
    if (!strcmp(argv[1], "--max-artifact")) {
      if (argc < 3 || !parse_limit(argv[2], &max_artifact_bytes))
        return cli_error("--max-artifact requires a non-negative byte limit");
      argv += 2; argc -= 2; continue;
    }
    break;
  }
  if (argc < 2) {
    usage();
    return 2;
  }
  if (!strcmp(argv[1], "--help") || !strcmp(argv[1], "-h")) {
    usage();
    return 0;
  }
  if (!strcmp(argv[1], "version") || !strcmp(argv[1], "--version")) {
    printf("Kryndel %s\n", KRY_VERSION);
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
    const char *root = getenv("KRY_ROOT");
    if (root && chdir(root) != 0)
      return cli_error("cannot enter the repository root for doctor");
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
  if (is_artifact((unsigned char *)data, length) ? length > max_artifact_bytes
                                                 : length > max_source_bytes) {
    error_set(&read_error, ERR_RESOURCE, NULL, 1, 1,
              "%s exceeds the configured input size limit", input);
    print_error(&read_error);
    free(data);
    return 1;
  }
  if (build) {
    if (is_artifact((unsigned char *)data, length)) {
      fprintf(stderr,
              "error[artifact]: build expects source, not an artifact\n");
      free(data);
      return 2;
    }
    char *default_path = NULL;
    if (!output) {
      default_path = default_output(input);
      output = default_path;
    }
    if (!output || same_existing_path(input, output)) {
      fprintf(stderr, "error[cli]: build input and output must be different paths\n");
      free(default_path);
      free(data);
      return 2;
    }
    if (restricted_mode) {
      char *safe_output = sandbox_resolve(output, false);
      if (!safe_output) {
        fprintf(stderr, "error[cli]: output path denied by sandbox\n");
        free(default_path);
        free(data);
        return 2;
      }
      output = safe_output;
    }
    Error write_error = {0};
    bool ok = build_artifact_source(input, data, length, output, &write_error);
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
    BundleEntry *entries = NULL;
    size_t entry_count = 0;
    char *payload = artifact_payload((unsigned char *)data, length,
                                     &payload_length, &entries, &entry_count,
                                     &artifact_error, input);
    free(data);
    if (!payload) {
      print_error(&artifact_error);
      return 1;
    }
    Arena artifact_arena = {0};
    Arena *previous_arena = arena_current;
    arena_current = &artifact_arena;
    Error run_error = {0};
    Program *program = load_root(input, payload, payload_length, entries,
                                 entry_count, &run_error);
    int result = 1;
    if (program && check_program(program, &run_error))
      result = check_only ? 0 : run_program(program);
    if (run_error.set)
      print_error(&run_error);
    arena_free(&artifact_arena);
    arena_current = previous_arena;
    for (size_t i = 0; i < entry_count; i++) {
      free(entries[i].logical_path);
      free(entries[i].data);
    }
    free(entries);
    free(payload);
    return result;
  }
  int result = process_source(input, data, length, check_only);
  free(data);
  return result;
}

static int process_repl_line(const char *data, size_t length, ReplSession *session) {
  Arena *previous = arena_current;
  arena_current = &session->arena;
  Error error = {0};
  session->runtime.error = (Error){0};
  Program *program = load_root("<repl>", data, length, NULL, 0, &error);
  Program *combined = NULL;
  if (program) {
    combined = aalloc(sizeof(*combined));
    *combined = *program;
    size_t count = session->function_count + program->function_count;
    if (count) {
      combined->functions = aalloc(count * sizeof(Function *));
      if (session->function_count)
        memcpy(combined->functions, session->functions,
               session->function_count * sizeof(Function *));
      if (program->function_count)
        memcpy(combined->functions + session->function_count, program->functions,
               program->function_count * sizeof(Function *));
      combined->function_count = count;
    }
    count = session->structure_count + program->structure_count;
    if (count) {
      combined->structures = aalloc(count * sizeof(StructDecl *));
      if (session->structure_count)
        memcpy(combined->structures, session->structures,
               session->structure_count * sizeof(StructDecl *));
      if (program->structure_count)
        memcpy(combined->structures + session->structure_count, program->structures,
               program->structure_count * sizeof(StructDecl *));
      combined->structure_count = count;
    }
    count = session->enumeration_count + program->enumeration_count;
    if (count) {
      combined->enumerations = aalloc(count * sizeof(EnumDecl *));
      if (session->enumeration_count)
        memcpy(combined->enumerations, session->enumerations,
               session->enumeration_count * sizeof(EnumDecl *));
      if (program->enumeration_count)
        memcpy(combined->enumerations + session->enumeration_count,
               program->enumerations, program->enumeration_count * sizeof(EnumDecl *));
      combined->enumeration_count = count;
    }
  }
  int result = 1;
  if (combined && check_program_in_scope(combined, &session->type_scope, &error)) {
    session->runtime.program = combined;
    if (!session->initialized) {
      session->runtime.global = runtime_scope(NULL);
      session->initialized = true;
    }
    if (program->statement_count == 1 && program->statements[0]->kind == ST_EXPR) {
      Value value = eval_expr(&session->runtime, session->runtime.global,
                              program->statements[0]->as.expression);
      if (session->runtime.error.set)
        error = session->runtime.error;
      else if (!print_value(stdout, value) || fputc('\n', stdout) == EOF ||
               fflush(stdout) != 0)
        error_set(&error, ERR_IO, program->statements[0]->source,
                  program->statements[0]->line, program->statements[0]->column,
                  "failed to write standard output");
      else
        result = 0;
    } else {
      session->functions = agrow(session->functions, session->function_count,
                                 &session->function_capacity,
                                 session->function_count + program->function_count,
                                 sizeof(Function *));
      if (program->function_count)
        memcpy(session->functions + session->function_count, program->functions,
               program->function_count * sizeof(Function *));
      session->function_count += program->function_count;
      session->structures = agrow(session->structures, session->structure_count,
                                  &session->structure_capacity,
                                  session->structure_count + program->structure_count,
                                  sizeof(StructDecl *));
      if (program->structure_count)
        memcpy(session->structures + session->structure_count, program->structures,
               program->structure_count * sizeof(StructDecl *));
      session->structure_count += program->structure_count;
      session->enumerations = agrow(session->enumerations, session->enumeration_count,
                                    &session->enumeration_capacity,
                                    session->enumeration_count + program->enumeration_count,
                                    sizeof(EnumDecl *));
      if (program->enumeration_count)
        memcpy(session->enumerations + session->enumeration_count,
               program->enumerations,
               program->enumeration_count * sizeof(EnumDecl *));
      session->enumeration_count += program->enumeration_count;
      result = 0;
      for (size_t i = 0; i < program->statement_count; i++) {
        ExecResult execution = execute_statement(&session->runtime,
                                                 session->runtime.global,
                                                 program->statements[i]);
        if (execution.code == EXEC_RETURN || execution.code == EXEC_BREAK ||
            execution.code == EXEC_CONTINUE) {
          error_set(&session->runtime.error, ERR_RUNTIME,
                    program->statements[i]->source, program->statements[i]->line,
                    program->statements[i]->column,
                    "control flow is not valid at the REPL top level");
          break;
        }
        if (session->runtime.error.set) break;
      }
      if (session->runtime.error.set) { error = session->runtime.error; result = 1; }
    }
  }
  if (error.set) print_error(&error);
  arena_current = previous;
  return result;
}
