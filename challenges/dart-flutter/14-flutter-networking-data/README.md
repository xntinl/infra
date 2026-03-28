# Section 14: Flutter Networking, HTTP & Data Serialization

## Introduction

Every meaningful mobile app talks to something -- a REST API, a local database, a WebSocket stream. Networking and data serialization are not features you bolt on at the end; they are architectural decisions that ripple through your entire codebase. Get them wrong and every screen directly calls `http.get`, parses JSON inline, and crashes silently when the user loses connectivity.

This section teaches you to build a data layer that is testable, resilient, and maintainable. You will start with raw HTTP and manual JSON, graduate to code-generated serialization, implement the repository pattern, and tackle real-world problems: pagination, offline-first, conflict resolution, and real-time data.

## Prerequisites

- **Sections 09-13**: Flutter setup, layouts, navigation, state basics, forms
- **Section 05**: Dart async fundamentals (Futures, async/await, Streams)

## Learning Objectives

1. **Execute** HTTP requests (GET, POST, PUT, DELETE) using `http` and Dio with headers, timeouts, and cancellation
2. **Implement** JSON serialization manually and with code generation (`json_serializable`, `freezed`)
3. **Design** a repository pattern abstracting remote and local data sources
4. **Construct** loading/success/error state handling with meaningful user feedback
5. **Build** pagination patterns (offset, cursor, infinite scroll) that scale
6. **Evaluate** offline-first strategies: local caching, sync queues, conflict resolution
7. **Integrate** WebSocket connections for real-time data with reconnection logic
8. **Compare** REST and GraphQL client approaches for different use cases

---

## Core Concepts

### 1. HTTP Basics with the http Package

```dart
// http_basics.dart
import 'dart:convert';
import 'package:http/http.dart' as http;

Future<void> fetchUsers() async {
  final uri = Uri.parse('https://jsonplaceholder.typicode.com/users');
  final response = await http.get(uri, headers: {
    'Accept': 'application/json',
    'Authorization': 'Bearer your-token-here',
  }).timeout(const Duration(seconds: 10),
      onTimeout: () => http.Response('{"error": "timeout"}', 408));

  if (response.statusCode == 200) {
    final List<dynamic> data = jsonDecode(response.body);
    print('Fetched ${data.length} users');
  } else {
    print('Request failed: ${response.statusCode}');
  }
}

Future<void> createUser(String name, String email) async {
  final response = await http.post(
    Uri.parse('https://jsonplaceholder.typicode.com/users'),
    headers: {'Content-Type': 'application/json'},  // forgetting this is the #1 bug
    body: jsonEncode({'name': name, 'email': email}),
  );
  if (response.statusCode == 201) print('Created: ${jsonDecode(response.body)['id']}');
}
```

### 2. Dio: Production-Grade HTTP Client

Dio adds interceptors, cancellation, upload progress, and structured errors. The cancel token matters -- when a user navigates away, cancel pending requests to avoid state updates on disposed widgets.

```dart
// dio_client.dart
import 'package:dio/dio.dart';

class ApiClient {
  late final Dio _dio;

  ApiClient({required String baseUrl, String? authToken}) {
    _dio = Dio(BaseOptions(
      baseUrl: baseUrl,
      connectTimeout: const Duration(seconds: 10),
      receiveTimeout: const Duration(seconds: 15),
    ));
    _dio.interceptors.add(InterceptorsWrapper(
      onRequest: (options, handler) {
        if (authToken != null) options.headers['Authorization'] = 'Bearer $authToken';
        handler.next(options);
      },
      onError: (error, handler) {
        if (error.response?.statusCode == 401) print('Token expired');
        handler.next(error);
      },
    ));
  }

  Future<List<Map<String, dynamic>>> getUsers({CancelToken? cancelToken}) async {
    final response = await _dio.get('/users', cancelToken: cancelToken);
    return List<Map<String, dynamic>>.from(response.data);
  }
}
```

### 3. JSON Serialization: Manual vs Generated

Manual `fromJson`/`toJson` works for small models but becomes a maintenance burden -- you will forget to update serialization when adding fields.

```dart
// user_model_generated.dart
import 'package:json_annotation/json_annotation.dart';
part 'user_model_generated.g.dart';

@JsonSerializable()
class User {
  final int id;
  final String name;
  @JsonKey(name: 'created_at') final DateTime createdAt;
  @JsonKey(defaultValue: false) final bool isVerified;

  const User({required this.id, required this.name, required this.createdAt, this.isVerified = false});
  factory User.fromJson(Map<String, dynamic> json) => _$UserFromJson(json);
  Map<String, dynamic> toJson() => _$UserToJson(this);
}
```

### 4. Freezed for Immutable Data Classes

Freezed generates immutable classes with `copyWith`, equality, and serialization from a single declaration. Immutability prevents bugs where one layer modifies an object another layer is still reading.

```dart
// user_freezed.dart
import 'package:freezed_annotation/freezed_annotation.dart';
part 'user_freezed.freezed.dart';
part 'user_freezed.g.dart';

@freezed
class User with _$User {
  const factory User({required int id, required String name, @Default(false) bool isVerified}) = _User;
  factory User.fromJson(Map<String, dynamic> json) => _$UserFromJson(json);
}
```

### 5. Network Error Handling and Result Types

```dart
// network_result.dart
sealed class NetworkError { final String message; const NetworkError(this.message); }
class ConnectionError extends NetworkError { const ConnectionError() : super('No internet connection'); }
class TimeoutError extends NetworkError { const TimeoutError() : super('Request timed out'); }
class ServerError extends NetworkError { final int code; const ServerError(this.code) : super('Server error'); }

sealed class Result<T> { const Result(); }
class Success<T> extends Result<T> { final T data; const Success(this.data); }
class Failure<T> extends Result<T> { final NetworkError error; const Failure(this.error); }
class Loading<T> extends Result<T> { const Loading(); }
```

### 6. Repository Pattern

A repository sits between your UI and data sources. The UI asks for users -- it does not know if they come from REST, a database, or cache.

```dart
// user_repository.dart
class UserRepositoryImpl implements UserRepository {
  final ApiClient _remote;
  final UserLocalDataSource _local;
  UserRepositoryImpl({required ApiClient remote, required UserLocalDataSource local})
      : _remote = remote, _local = local;

  Future<Result<List<User>>> getUsers() async {
    try {
      final users = await _remote.getUsers();
      await _local.cacheUsers(users);
      return Success(users);
    } on DioException catch (e) {
      final cached = await _local.getCachedUsers();
      if (cached.isNotEmpty) return Success(cached);
      return Failure(_mapError(e));
    }
  }
}
```

### 7. Pagination Patterns

Offset-based is simple but breaks when data changes between pages (duplicates, missed items). Cursor-based tracks position by a stable identifier.

### 8. Image Loading

Use `cached_network_image` for disk caching, placeholders during loading, and error widgets for failures.

### 9. Local Storage

SharedPreferences for key-value settings. Hive for schema-less NoSQL. drift/sqflite for relational data with type-safe queries. Choose based on data shape, not popularity.

### 10. Offline-First Architecture

Write locally first, enqueue a sync operation, process the queue when online. The user gets instant feedback; sync happens transparently.

### 11. WebSockets

For real-time features (chat, dashboards), WebSockets maintain a persistent connection. Always implement automatic reconnection with exponential backoff -- mobile connections drop frequently.

### 12. REST vs GraphQL

REST works when resources map to CRUD and each screen needs roughly what one endpoint returns. GraphQL shines when screens need data from multiple related resources or clients have different data requirements.

---

## Exercises

### Exercise 1 (Basic): HTTP GET with Loading and Error States

**Estimated time: 30 minutes**

Build a screen that fetches posts from `https://jsonplaceholder.typicode.com/posts` and displays them with loading, success, and error states.

**Instructions:**
1. Create a `Post` model with manual `fromJson` (id, userId, title, body)
2. Build a screen with three states: spinner, list, error with retry button
3. Add a 10-second timeout and proper headers

```dart
// exercise_01.dart
class Post {
  final int id, userId;
  final String title, body;
  const Post({required this.id, required this.userId, required this.title, required this.body});
  factory Post.fromJson(Map<String, dynamic> json) {
    // TODO: parse all fields, casting from dynamic
    throw UnimplementedError();
  }
}

class _PostListScreenState extends State<PostListScreen> {
  List<Post>? _posts;
  String? _error;
  bool _isLoading = false;

  Future<void> _fetchPosts() async {
    setState(() { _isLoading = true; _error = null; });
    // TODO: GET request with headers, timeout. Parse JSON list into Posts.
    // On failure, capture error. Always set _isLoading = false.
  }

  Widget build(BuildContext context) {
    // TODO: Scaffold with AppBar, body switching on _isLoading/_error/_posts
    throw UnimplementedError();
  }
}
```

**Verification:** Spinner appears during load. 100 posts display. Airplane mode + Retry shows error. Disable airplane mode + Retry reloads.

---

### Exercise 2 (Basic): JSON Serialization with json_serializable

**Estimated time: 30 minutes**

Convert manual Post model to use `json_serializable`. Add a nested `Author` model. Use `@JsonKey` for snake_case mapping and default values.

```dart
// exercise_02.dart
import 'package:json_annotation/json_annotation.dart';
part 'exercise_02.g.dart';

@JsonSerializable()
class Author {
  final int id;
  final String name, email, username;
  // TODO: constructor, fromJson, toJson using generated functions
}

@JsonSerializable()
class Post {
  final int id;
  final String title, body;
  @JsonKey(name: 'user_id') final int userId;
  @JsonKey(name: 'created_at') final DateTime? createdAt;
  @JsonKey(defaultValue: 0) final int likesCount;
  // TODO: constructor, fromJson, toJson
}
```

**Verification:** `dart run build_runner build` generates `.g.dart` files. Round-trip test: `Post.fromJson(json).toJson()` preserves all fields. `likesCount` defaults to 0 when missing from JSON.

---

### Exercise 3 (Intermediate): Repository with Local Cache

**Estimated time: 45 minutes**

Implement a `UserRepository` that fetches from a remote API and caches locally. When the network is unavailable, transparently return cached data.

```dart
// exercise_03.dart
abstract class UserRepository {
  Future<Result<List<User>>> getUsers();
  Future<Result<User>> getUserById(int id);
  Future<Result<List<User>>> refreshUsers();
}

class LocalUserDataSource {
  final Map<int, User> _cache = {};
  // TODO: cacheUsers, getCachedUsers, getCachedUserById, clearCache
}

class UserRepositoryImpl implements UserRepository {
  // TODO: Try remote first, cache on success. On network failure,
  // fall back to cache. If cache empty, return Failure.
}
```

**Verification:** Test three scenarios: (1) working network returns Success and populates cache, (2) network failure returns Success from cache, (3) network failure with empty cache returns Failure.

---

### Exercise 4 (Intermediate): Infinite Scroll Pagination

**Estimated time: 45 minutes**

Build a paginated list that loads more items as the user scrolls to the bottom using `?_page=N&_limit=10`.

```dart
// exercise_04.dart
class PaginatedPostsController extends ChangeNotifier {
  final List<Post> _posts = [];
  int _currentPage = 0;
  bool _hasMore = true, _isLoading = false;

  Future<void> loadNextPage() async {
    if (_isLoading || !_hasMore) return;
    // TODO: Fetch next page, append, set _hasMore = results.length == limit
  }
}

class _PaginatedPostsScreenState extends State<PaginatedPostsScreen> {
  void _onScroll() {
    // TODO: Detect within 200px of bottom, trigger loadNextPage
  }

  Widget build(BuildContext context) {
    // TODO: ListView.builder with itemCount = posts.length + (hasMore ? 1 : 0)
    // Extra item is a loading indicator at the bottom
  }
}
```

**Verification:** First 10 posts load. Scrolling near bottom triggers next page with spinner. After all 100 posts (10 pages), no more spinners. Pull-to-refresh resets.

---

### Exercise 5 (Advanced): Offline-First with Sync Queue

**Estimated time: 90 minutes**

Build offline-first task management. Users create/update/delete tasks offline; changes queue and sync when connectivity returns.

```dart
// exercise_05.dart
enum SyncStatus { synced, pending, failed }

class Task {
  final String id, title;
  final bool isCompleted;
  final DateTime updatedAt;
  final SyncStatus syncStatus;
  // TODO: constructor, copyWith, fromJson, toJson
}

class SyncQueue {
  final List<SyncOperation> _pending = [];
  final List<SyncOperation> _deadLetter = [];
  // TODO: enqueue, processAll with retry logic (3 max), dead letter handling
}

class TaskRepository {
  // TODO: createTask writes to local DB with SyncStatus.pending,
  // enqueues sync operation, returns immediately.
  // syncPending() processes queue when online.
}
```

**Verification:** Create 3 tasks offline (pending status). Call syncPending (status becomes synced). Simulate sync failure -- retry count increments. After 3 failures, operation moves to dead letter queue.

---

### Exercise 6 (Advanced): WebSocket Real-Time Chat

**Estimated time: 90 minutes**

Build real-time chat with WebSocket connections, automatic reconnection with exponential backoff, and an outbox for messages sent while disconnected.

```dart
// exercise_06.dart
enum MessageStatus { sending, sent, delivered, failed }

class WebSocketChatClient {
  final List<ChatMessage> _outbox = [];
  int _reconnectAttempts = 0;

  void connect(String url) {
    // TODO: Connect, listen for messages, handle errors with reconnection, flush outbox
  }
  void sendMessage(ChatMessage message) {
    // TODO: If connected, send. If disconnected, add to outbox.
  }
  void _scheduleReconnect() {
    // TODO: Exponential backoff: min(pow(2, attempts), 30) seconds
  }
}
```

**Verification:** Send message (sent status). Simulate disconnect (messages queue). Reconnect (outbox flushes). Verify exponential backoff timing.

---

### Exercise 7 (Insane): Full Offline-First Data Synchronization Engine

**Estimated time: 4+ hours**

Build a sync engine with CRDT-inspired conflict resolution, delta sync, persistent retry queue, and connectivity-aware batch processing.

```dart
// exercise_07.dart
class VectorClock {
  final Map<String, int> _clocks;
  void increment(String nodeId) { /* TODO */ }
  bool happenedBefore(VectorClock other) {
    // TODO: true if all entries <= other's and at least one strictly less
  }
  VectorClock merge(VectorClock other) {
    // TODO: new clock with max of each entry
  }
}

sealed class ConflictResult { const ConflictResult(); }
class AutoResolved extends ConflictResult { final dynamic resolvedValue; const AutoResolved(this.resolvedValue); }
class ManualResolutionNeeded extends ConflictResult {
  final dynamic localValue, remoteValue;
  const ManualResolutionNeeded(this.localValue, this.remoteValue);
}

class ConflictResolver {
  ConflictResult resolve(FieldChange local, FieldChange remote) {
    // TODO: Causal ordering via vector clocks. LWW fallback for concurrent.
    // Same-field concurrent with different values -> ManualResolutionNeeded.
  }
}

class DeltaSync {
  Map<String, dynamic> computeDelta(Map<String, dynamic> current, Map<String, dynamic> lastSynced) {
    // TODO: Return only changed/added/removed fields
  }
}

class SyncEngine {
  // TODO: Pull remote changes, detect conflicts, auto-resolve or flag,
  // compute deltas, push in batches sized by connection quality
}
```

**Verification:** (1) Causal ordering resolves same-field changes. (2) Different-field concurrent changes auto-merge. (3) Same-field concurrent changes flag for manual resolution. (4) Delta sync sends only changed fields. (5) Retry with exponential backoff, dead letter after max retries. (6) Batch sizes adjust per connection quality.

---

### Exercise 8 (Insane): GraphQL Client with Normalized Cache

**Estimated time: 4+ hours**

Build a GraphQL client with normalized caching -- entities stored as `TypeName:id`, so updating one entity automatically updates every query referencing it.

```dart
// exercise_08.dart
class NormalizedCache {
  final Map<String, CacheEntry> _store = {};
  final Map<String, dynamic> _queryResults = {};

  void writeQuery(String queryKey, Map<String, dynamic> data) {
    // TODO: Walk response. Objects with __typename + id -> extract to _store,
    // replace inline with CacheReference. Track which queries reference which entries.
  }
  Map<String, dynamic>? readQuery(String queryKey) {
    // TODO: Reconstruct response by resolving CacheReferences. Return null if any missing.
  }
  void garbageCollect(String queryKey) {
    // TODO: Remove queryKey from referencedBy sets. Delete entries with no references.
  }
}

class GraphQLClient {
  Future<Map<String, dynamic>> query(GraphQLQuery query) async {
    // TODO: Check cache first. On miss, fetch from network, normalize, return.
  }
  Stream<Map<String, dynamic>> watchQuery(GraphQLQuery query) {
    // TODO: Emit cached data, re-emit when referenced entities change.
  }
}
```

**Verification:** (1) Query response normalizes into cache entries by type+id. (2) Same query again serves from cache. (3) Mutation updates entity, all watchers re-emit. (4) Dispose watcher, garbage collection removes unreferenced entries. (5) Two queries referencing `User:5` both update when that user is mutated.

---

## Summary

This section covered the full data-handling spectrum: HTTP fundamentals and production Dio usage, JSON serialization from manual to generated with Freezed, the repository pattern for clean data source abstraction, pagination strategies, local storage options, offline-first sync queues, WebSocket real-time connections, and REST vs GraphQL trade-offs. The unifying principle is separation of concerns -- your UI should never know how data reaches it.

## What's Next

**Section 15: Advanced State Management** connects the data layer you built here to state management solutions like Riverpod and Bloc. You will manage complex state across screens, handle optimistic updates, and implement reactive flows where UI rebuilds automatically when data changes.

## References

- [http package](https://pub.dev/packages/http) | [Dio](https://pub.dev/packages/dio) | [json_serializable](https://pub.dev/packages/json_serializable) | [Freezed](https://pub.dev/packages/freezed)
- [cached_network_image](https://pub.dev/packages/cached_network_image) | [shared_preferences](https://pub.dev/packages/shared_preferences) | [drift](https://pub.dev/packages/drift) | [hive](https://pub.dev/packages/hive)
- [web_socket_channel](https://pub.dev/packages/web_socket_channel) | [graphql_flutter](https://pub.dev/packages/graphql_flutter)
- [Flutter networking cookbook](https://docs.flutter.dev/cookbook/networking) | [JSON serialization guide](https://docs.flutter.dev/data-and-backend/serialization/json)
