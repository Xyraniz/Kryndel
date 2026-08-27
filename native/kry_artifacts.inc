static int bundle_entry_compare(const void *left, const void *right) {
  const BundleEntry *a = left, *b = right;
  return strcmp(a->logical_path, b->logical_path);
}
static bool artifact_name_safe(const char *name) {
  if (!name || !*name || name[0] == '/' || strchr(name, '\\')) return false;
  const char *start = name;
  for (const char *p = name;; p++) {
    if (*p == '/' || *p == 0) {
      size_t length = (size_t)(p - start);
      if (!length || (length == 1 && start[0] == '.') ||
          (length == 2 && start[0] == '.' && start[1] == '.')) return false;
      if (!*p) break;
      start = p + 1;
    }
  }
  return true;
}
static char *artifact_payload(const unsigned char *data, size_t length,
                              size_t *payload_length, BundleEntry **entries,
                              size_t *entry_count, Error *error,
                              const char *path) {
  size_t magic_length = sizeof(artifact_magic) - 1;
  if (length < magic_length + 4 + 4 + 8 + 8 + 4) {
    error_set(error, ERR_ARTIFACT, NULL, 1, 1,
              "%s: malformed native artifact: truncated header", path);
    return NULL;
  }
  size_t offset = magic_length;
  uint32_t version = artifact_u32(data + offset); offset += 4;
  if (version != artifact_format_version) {
    error_set(error, ERR_ARTIFACT, NULL, 1, 1,
              "%s: incompatible native artifact format version %u", path, version);
    return NULL;
  }
  uint32_t compiler_length = artifact_u32(data + offset); offset += 4;
  if (compiler_length > length - offset || compiler_length != strlen(artifact_compiler_version) ||
      memcmp(data + offset, artifact_compiler_version, compiler_length)) {
    error_set(error, ERR_ARTIFACT, NULL, 1, 1,
              "%s: incompatible compiler version in artifact", path);
    return NULL;
  }
  offset += compiler_length;
  if (offset + 4 > length) goto truncated;
  uint32_t target_length = artifact_u32(data + offset); offset += 4;
  if (target_length > length - offset || target_length != strlen(artifact_target) ||
      memcmp(data + offset, artifact_target, target_length)) {
    error_set(error, ERR_ARTIFACT, NULL, 1, 1,
              "%s: incompatible target in artifact", path);
    return NULL;
  }
  offset += target_length;
  if (offset + 4 + 8 > length) goto truncated;
  uint32_t count = artifact_u32(data + offset); offset += 4;
  uint64_t payload_size = artifact_u64(data + offset); offset += 8;
  if (count == 0 || payload_size > SIZE_MAX ||
      payload_size != length - offset) {
    error_set(error, ERR_ARTIFACT, NULL, 1, 1,
              "%s: malformed native artifact: payload length does not match file length", path);
    return NULL;
  }
  BundleEntry *decoded = calloc(count, sizeof(*decoded));
  if (!decoded) { error_set(error, ERR_RESOURCE, NULL, 1, 1, "artifact metadata is too large"); return NULL; }
  char *root = NULL;
  size_t root_size = 0;
  for (uint32_t i = 0; i < count; i++) {
    if (length - offset < 4 + 8 + 32) goto malformed;
    uint32_t name_length = artifact_u32(data + offset); offset += 4;
    uint64_t source_size = artifact_u64(data + offset); offset += 8;
    if (name_length == 0 || name_length > length - offset ||
        length - offset - name_length < 32 || source_size > SIZE_MAX ||
        source_size > length - offset - name_length - 32) goto malformed;
    decoded[i].logical_path = malloc((size_t)name_length + 1);
    decoded[i].data = malloc((size_t)source_size + 1);
    if (!decoded[i].logical_path || !decoded[i].data) goto malformed;
    memcpy(decoded[i].logical_path, data + offset, name_length);
    if (memchr(decoded[i].logical_path, '\0', name_length)) goto malformed;
    decoded[i].logical_path[name_length] = 0; offset += name_length;
    if ((i == 0 && strcmp(decoded[i].logical_path, "<root>")) ||
        (i != 0 && !artifact_name_safe(decoded[i].logical_path))) goto malformed;
    memcpy(decoded[i].data, data + offset, (size_t)source_size);
    decoded[i].data[source_size] = 0; decoded[i].length = (size_t)source_size; offset += (size_t)source_size;
    unsigned char actual[32]; Sha256 sha; sha256_init(&sha);
    sha256_update(&sha, (const unsigned char *)decoded[i].data, decoded[i].length);
    sha256_final(&sha, actual);
    if (memcmp(actual, data + offset, 32)) goto malformed;
    offset += 32;
    for (uint32_t prior = 0; prior < i; prior++)
      if (!strcmp(decoded[prior].logical_path, decoded[i].logical_path)) goto malformed;
    if (i == 0) { root = decoded[i].data; root_size = decoded[i].length; }
  }
  if (offset != length || !root) goto malformed;
  *payload_length = root_size;
  *entries = decoded; *entry_count = count;
  char *result = malloc(root_size + 1);
  if (!result) { error_set(error, ERR_RESOURCE, NULL, 1, 1, "artifact root source is too large"); return NULL; }
  memcpy(result, root, root_size + 1);
  return result;
truncated:
  error_set(error, ERR_ARTIFACT, NULL, 1, 1, "%s: malformed native artifact: truncated header", path);
  return NULL;
malformed:
  for (uint32_t i = 0; i < count; i++) { free(decoded[i].logical_path); free(decoded[i].data); }
  free(decoded);
  error_set(error, ERR_ARTIFACT, NULL, 1, 1, "%s: malformed native artifact entry", path);
  return NULL;
}
static char *artifact_logical_name(Program *program, Source *source) {
  if (!program || !program->source_count || source == program->sources[0])
    return astr("<root>");
  const char *root = program->sources[0]->name;
  const char *slash = strrchr(root, '/');
  size_t root_length = slash ? (size_t)(slash - root) : 0;
  if (root_length && !strncmp(source->name, root, root_length) &&
      source->name[root_length] == '/')
    return astr(source->name + root_length + 1);
  slash = strrchr(source->name, '/');
  return astr(slash ? slash + 1 : source->name);
}
static bool write_artifact(const char *path, Program *program, Error *error) {
  if (!program || !program->source_count || program->source_count > UINT32_MAX) {
    error_set(error, ERR_ARTIFACT, NULL, 1, 1,
              "artifact requires at least one source entry");
    return false;
  }
  BundleEntry *entries = calloc(program->source_count, sizeof(*entries));
  if (!entries) {
    error_set(error, ERR_RESOURCE, NULL, 1, 1, "artifact metadata is too large");
    return false;
  }
  for (size_t i = 0; i < program->source_count; i++) {
    entries[i].logical_path = artifact_logical_name(program, program->sources[i]);
    entries[i].data = program->sources[i]->text;
    entries[i].length = program->sources[i]->length;
  }
  qsort(entries, program->source_count, sizeof(*entries), bundle_entry_compare);
  size_t payload_size = 0;
  for (size_t i = 0; i < program->source_count; i++) {
    size_t entry = 4 + 8 + 32;
    if (!size_add(entry, strlen(entries[i].logical_path), &entry) ||
        !size_add(entry, entries[i].length, &entry) ||
        !size_add(payload_size, entry, &payload_size)) {
      free(entries); error_set(error, ERR_RESOURCE, NULL, 1, 1,
                                "artifact is too large"); return false;
    }
  }
  size_t fixed = sizeof(artifact_magic) - 1 + 4 + 4 + strlen(artifact_compiler_version) +
                 4 + strlen(artifact_target) + 4 + 8;
  size_t total = 0;
  if (!size_add(fixed, payload_size, &total)) {
    free(entries); error_set(error, ERR_RESOURCE, NULL, 1, 1, "artifact is too large"); return false;
  }
  (void)total;
  size_t temp_length = strlen(path) + 12;
  char *temporary = malloc(temp_length);
  if (!temporary) { free(entries); error_set(error, ERR_RESOURCE, NULL, 1, 1, "artifact path is too long"); return false; }
  snprintf(temporary, temp_length, "%s.tmp.XXXXXX", path);
  int descriptor = mkstemp(temporary);
  if (descriptor < 0) {
    free(entries); error_set(error, ERR_IO, NULL, 1, 1, "cannot create temporary artifact '%s': %s", path, strerror(errno)); return false;
  }
  FILE *file = fdopen(descriptor, "wb");
  if (!file) { close(descriptor); unlink(temporary); free(temporary); free(entries); error_set(error, ERR_IO, NULL, 1, 1, "cannot open temporary artifact '%s': %s", path, strerror(errno)); return false; }
  bool ok = fwrite(artifact_magic, 1, sizeof(artifact_magic) - 1, file) == sizeof(artifact_magic) - 1;
  artifact_put_u32(file, artifact_format_version, &ok);
  artifact_put_u32(file, (uint32_t)strlen(artifact_compiler_version), &ok);
  if (ok && fwrite(artifact_compiler_version, 1, strlen(artifact_compiler_version), file) != strlen(artifact_compiler_version)) ok = false;
  artifact_put_u32(file, (uint32_t)strlen(artifact_target), &ok);
  if (ok && fwrite(artifact_target, 1, strlen(artifact_target), file) != strlen(artifact_target)) ok = false;
  artifact_put_u32(file, (uint32_t)program->source_count, &ok);
  artifact_put_u64(file, (uint64_t)payload_size, &ok);
  for (size_t i = 0; i < program->source_count && ok; i++) {
    size_t name_length = strlen(entries[i].logical_path);
    artifact_put_u32(file, (uint32_t)name_length, &ok);
    artifact_put_u64(file, (uint64_t)entries[i].length, &ok);
    if (ok && fwrite(entries[i].logical_path, 1, name_length, file) != name_length) ok = false;
    if (ok && fwrite(entries[i].data, 1, entries[i].length, file) != entries[i].length) ok = false;
    /* Hash the exact bytes represented by this sorted entry. */
    unsigned char hash[32];
    Sha256 sha; sha256_init(&sha);
    sha256_update(&sha, (const unsigned char *)entries[i].data, entries[i].length);
    sha256_final(&sha, hash);
    if (ok && fwrite(hash, 1, sizeof(hash), file) != sizeof(hash)) ok = false;
  }
  if (ok && fflush(file) != 0) ok = false;
  if (ok && fsync(fileno(file)) != 0) ok = false;
  if (fclose(file) != 0) ok = false;
  if (!ok || rename(temporary, path) != 0) {
    unlink(temporary); free(temporary); free(entries);
    error_set(error, ERR_IO, NULL, 1, 1, "failed while writing '%s' atomically", path);
    return false;
  }
  free(temporary); free(entries);
  return true;
}
