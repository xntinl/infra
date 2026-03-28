# Section 14: Solutions -- Flutter Networking, HTTP & Data Serialization

## How to Use This File

For each exercise: progressive hints (read one at a time), full solution, common mistakes, and deeper context. Try each exercise yourself first. Read one hint, try again. The full solution is for when you genuinely need it.

---

## Exercise 1: HTTP GET with Loading and Error States

### Hints

1. `http.get` returns `Future<Response>`. Use `jsonDecode(response.body)` to get a `List<dynamic>`, then map each element through `Post.fromJson`.
2. Your three states (`_isLoading`, `_error`, `_posts`) are checked in order in `build`: loading -> error -> list. Call `setState` every time you change them.
3. Wrap the entire fetch in try/catch. Catch `http.ClientException` (network) and `FormatException` (parse). Use `finally` for `_isLoading = false`.

### Solution

```dart
// exercise_01_solution.dart
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

class Post {
  final int id, userId;
  final String title, body;
  const Post({required this.id, required this.userId, required this.title, required this.body});

  factory Post.fromJson(Map<String, dynamic> json) => Post(
    id: json['id'] as int, userId: json['userId'] as int,
    title: json['title'] as String, body: json['body'] as String,
  );
}

class _PostListScreenState extends State<PostListScreen> {
  List<Post>? _posts;
  String? _error;
  bool _isLoading = false;

  @override
  void initState() { super.initState(); _fetchPosts(); }

  Future<void> _fetchPosts() async {
    setState(() { _isLoading = true; _error = null; });
    try {
      final response = await http.get(
        Uri.parse('https://jsonplaceholder.typicode.com/posts'),
        headers: {'Accept': 'application/json'},
      ).timeout(const Duration(seconds: 10));

      if (response.statusCode == 200) {
        final List<dynamic> data = jsonDecode(response.body);
        setState(() { _posts = data.map((j) => Post.fromJson(j as Map<String, dynamic>)).toList(); });
      } else {
        setState(() { _error = 'Server returned ${response.statusCode}'; });
      }
    } on http.ClientException catch (e) {
      setState(() { _error = 'Network error: ${e.message}'; });
    } catch (e) {
      setState(() { _error = 'Unexpected error: $e'; });
    } finally {
      setState(() { _isLoading = false; });
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Posts')),
    body: _isLoading
        ? const Center(child: CircularProgressIndicator())
        : _error != null
            ? Center(child: Column(mainAxisAlignment: MainAxisAlignment.center, children: [
                Text(_error!, style: const TextStyle(color: Colors.red)),
                const SizedBox(height: 16),
                ElevatedButton(onPressed: _fetchPosts, child: const Text('Retry')),
              ]))
            : RefreshIndicator(
                onRefresh: _fetchPosts,
                child: ListView.builder(
                  itemCount: _posts?.length ?? 0,
                  itemBuilder: (ctx, i) => ListTile(
                    title: Text(_posts![i].title, maxLines: 1, overflow: TextOverflow.ellipsis),
                    subtitle: Text(_posts![i].body, maxLines: 2, overflow: TextOverflow.ellipsis),
                  ),
                ),
              ),
  );
}
```

### Common Mistakes

- **Not calling setState**: Assigning `_posts = data` without `setState` means the widget never rebuilds. Screen stays blank.
- **No finally block**: If the try throws before setting `_isLoading = false`, the spinner spins forever.
- **Wrong cast types**: The API returns `userId` as `int`. Casting `json['userId'] as String` gives a runtime error.

---

## Exercise 2: JSON Serialization with json_serializable

### Hints

1. The `part` directive must match your filename: `part 'exercise_02.g.dart';`. Without it, build_runner generates nothing.
2. Generated functions follow the pattern `_$ClassNameFromJson` / `_$ClassNameToJson`.
3. If build_runner fails with "Could not generate fromJson", check that nested types also have `@JsonSerializable()`.

### Solution

```dart
// exercise_02_solution.dart
import 'package:json_annotation/json_annotation.dart';
part 'exercise_02_solution.g.dart';

@JsonSerializable()
class Author {
  final int id;
  final String name, email, username;
  const Author({required this.id, required this.name, required this.email, required this.username});
  factory Author.fromJson(Map<String, dynamic> json) => _$AuthorFromJson(json);
  Map<String, dynamic> toJson() => _$AuthorToJson(this);
}

@JsonSerializable()
class Post {
  final int id;
  final String title, body;
  @JsonKey(name: 'user_id') final int userId;
  @JsonKey(name: 'created_at') final DateTime? createdAt;
  @JsonKey(defaultValue: 0) final int likesCount;
  const Post({required this.id, required this.title, required this.body,
    required this.userId, this.createdAt, this.likesCount = 0});
  factory Post.fromJson(Map<String, dynamic> json) => _$PostFromJson(json);
  Map<String, dynamic> toJson() => _$PostToJson(this);
}

// Verification test
void main() {
  final json = {'id': 1, 'title': 'Test', 'body': 'Content', 'user_id': 42, 'created_at': '2026-03-28T10:00:00Z'};
  final post = Post.fromJson(json);
  assert(post.userId == 42);
  assert(post.likesCount == 0); // default applied
  assert(post.toJson()['user_id'] == 42); // snake_case preserved
  print('All serialization tests passed');
}
```

### Common Mistakes

- **Forgetting `part` directive**: No generated code, undefined function errors.
- **Not re-running build_runner after changes**: Old `.g.dart` file misses new fields. Silent data loss.
- **Mismatching @JsonKey name with actual API**: If the API sends `userId` but you wrote `@JsonKey(name: 'user_id')`, the field is always null.

### Deep Dive: json_serializable vs Freezed

Use `json_serializable` when you only need serialization. Use Freezed when you also want immutability, `copyWith`, sealed unions, or deep equality. Freezed generates more code and adds build time.

---

## Exercise 3: Repository with Local Cache

### Hints

1. The remote data source should throw on errors. The repository catches exceptions and decides whether to fall back to cache.
2. The local data source is an in-memory `Map<int, User>` with `cacheUsers`, `getCachedUsers`, `getCachedUserById`.
3. Flow: try remote -> on success, cache and return Success -> on exception, try cache -> if non-empty, return Success -> if empty, return Failure.

### Solution

```dart
// exercise_03_solution.dart
import 'package:dio/dio.dart';

class User {
  final int id;
  final String name, email;
  const User({required this.id, required this.name, required this.email});
  factory User.fromJson(Map<String, dynamic> json) =>
      User(id: json['id'] as int, name: json['name'] as String, email: json['email'] as String);
}

sealed class Result<T> { const Result(); }
class Success<T> extends Result<T> { final T data; const Success(this.data); }
class Failure<T> extends Result<T> { final String message; const Failure(this.message); }

class RemoteUserDataSource {
  final Dio _dio;
  RemoteUserDataSource(this._dio);
  Future<List<User>> fetchUsers() async {
    final response = await _dio.get('/users');
    return (response.data as List).map((j) => User.fromJson(j)).toList();
  }
  Future<User> fetchUserById(int id) async {
    final response = await _dio.get('/users/$id');
    return User.fromJson(response.data);
  }
}

class LocalUserDataSource {
  final Map<int, User> _cache = {};
  Future<void> cacheUsers(List<User> users) async { for (final u in users) _cache[u.id] = u; }
  Future<void> cacheUser(User user) async { _cache[user.id] = user; }
  Future<List<User>> getCachedUsers() async => _cache.values.toList();
  Future<User?> getCachedUserById(int id) async => _cache[id];
  Future<void> clearCache() async { _cache.clear(); }
  bool get isEmpty => _cache.isEmpty;
}

class UserRepositoryImpl {
  final RemoteUserDataSource _remote;
  final LocalUserDataSource _local;
  UserRepositoryImpl({required RemoteUserDataSource remote, required LocalUserDataSource local})
      : _remote = remote, _local = local;

  Future<Result<List<User>>> getUsers() async {
    try {
      final users = await _remote.fetchUsers();
      await _local.cacheUsers(users);
      return Success(users);
    } on DioException catch (e) {
      final cached = await _local.getCachedUsers();
      if (cached.isNotEmpty) return Success(cached);
      return Failure(_describe(e));
    }
  }

  Future<Result<User>> getUserById(int id) async {
    try {
      final user = await _remote.fetchUserById(id);
      await _local.cacheUser(user);
      return Success(user);
    } on DioException catch (e) {
      final cached = await _local.getCachedUserById(id);
      if (cached != null) return Success(cached);
      return Failure(_describe(e));
    }
  }

  Future<Result<List<User>>> refreshUsers() async {
    try {
      await _local.clearCache();
      final users = await _remote.fetchUsers();
      await _local.cacheUsers(users);
      return Success(users);
    } on DioException catch (e) {
      return Failure(_describe(e));
    }
  }

  String _describe(DioException e) => switch (e.type) {
    DioExceptionType.connectionTimeout => 'Connection timed out',
    DioExceptionType.connectionError => 'No internet',
    _ => 'Request failed: ${e.response?.statusCode ?? "unknown"}',
  };
}
```

### Common Mistakes

- **Clearing cache before confirming remote success in refreshUsers**: If clear + fetch fails, you lose both sources. The trade-off is intentional here for "force refresh" semantics.
- **Repository directly using Dio**: Inject data sources instead -- they are simpler to mock in tests.
- **Not indicating cached data is stale**: Consider adding an `isFromCache` flag so the UI can show "Last updated 2 hours ago."

---

## Exercise 4: Infinite Scroll Pagination

### Hints

1. Scroll detection: `position.pixels >= position.maxScrollExtent - 200`. The 200px threshold starts loading before the user hits bottom.
2. `itemCount = posts.length + (hasMore ? 1 : 0)`. The extra item is a `CircularProgressIndicator`.
3. Guard `loadNextPage` with `if (_isLoading || !_hasMore) return;` to prevent duplicate requests from fast scrolling.

### Solution

```dart
// exercise_04_solution.dart
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

class PaginatedPostsController extends ChangeNotifier {
  final List<Post> _posts = [];
  int _currentPage = 0;
  bool _hasMore = true, _isLoading = false;
  String? _error;
  static const _limit = 10;

  List<Post> get posts => List.unmodifiable(_posts);
  bool get hasMore => _hasMore;
  bool get isLoading => _isLoading;
  String? get error => _error;

  Future<void> loadNextPage() async {
    if (_isLoading || !_hasMore) return;
    _isLoading = true; _error = null; notifyListeners();

    try {
      final next = _currentPage + 1;
      final response = await http.get(
        Uri.parse('https://jsonplaceholder.typicode.com/posts?_page=$next&_limit=$_limit'),
      ).timeout(const Duration(seconds: 10));

      if (response.statusCode == 200) {
        final data = (jsonDecode(response.body) as List)
            .map((j) => Post.fromJson(j as Map<String, dynamic>)).toList();
        _posts.addAll(data);
        _currentPage = next;
        _hasMore = data.length == _limit;
      } else { _error = 'Status ${response.statusCode}'; }
    } catch (e) { _error = '$e'; }
    finally { _isLoading = false; notifyListeners(); }
  }

  Future<void> reset() async {
    _posts.clear(); _currentPage = 0; _hasMore = true; _error = null;
    notifyListeners();
    await loadNextPage();
  }
}

class _PaginatedPostsScreenState extends State<PaginatedPostsScreen> {
  final _ctrl = PaginatedPostsController();
  final _scroll = ScrollController();

  @override
  void initState() {
    super.initState();
    _ctrl.addListener(() => setState(() {}));
    _ctrl.loadNextPage();
    _scroll.addListener(() {
      if (_scroll.position.pixels >= _scroll.position.maxScrollExtent - 200) _ctrl.loadNextPage();
    });
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Posts')),
    body: RefreshIndicator(
      onRefresh: _ctrl.reset,
      child: _ctrl.posts.isEmpty && _ctrl.isLoading
          ? const Center(child: CircularProgressIndicator())
          : ListView.builder(
              controller: _scroll,
              itemCount: _ctrl.posts.length + (_ctrl.hasMore ? 1 : 0),
              itemBuilder: (ctx, i) => i >= _ctrl.posts.length
                  ? const Padding(padding: EdgeInsets.all(16), child: Center(child: CircularProgressIndicator()))
                  : ListTile(
                      leading: CircleAvatar(child: Text('${_ctrl.posts[i].id}')),
                      title: Text(_ctrl.posts[i].title, maxLines: 1, overflow: TextOverflow.ellipsis),
                    ),
            ),
    ),
  );

  @override
  void dispose() { _scroll.dispose(); _ctrl.dispose(); super.dispose(); }
}
```

### Common Mistakes

- **No concurrent-load guard**: Without `if (_isLoading) return`, fast scrolling fires multiple requests for the same page, creating duplicates.
- **Forgetting to dispose ScrollController**: Memory leak. Always dispose in `dispose()`.

### Alternative

Replace the `ScrollController` listener with `NotificationListener<ScrollNotification>` wrapping the `ListView` to avoid managing a controller.

---

## Exercise 5: Offline-First with Sync Queue

### Hints

1. Every write: local DB first, create SyncOperation, enqueue, return immediately. User never waits for the network.
2. `processAll` iterates pending ops, attempts each: success (remove), retryable failure (increment count), permanent failure after 3 retries (dead letter).
3. `ConnectivityMonitor` emits a stream. Repository listens and calls `syncPending()` on reconnection.

### Solution

```dart
// exercise_05_solution.dart
import 'dart:async';

enum SyncStatus { synced, pending, failed }

class Task {
  final String id, title;
  final bool isCompleted;
  final DateTime updatedAt;
  final SyncStatus syncStatus;
  const Task({required this.id, required this.title, required this.isCompleted,
    required this.updatedAt, required this.syncStatus});
  Task copyWith({String? title, bool? isCompleted, DateTime? updatedAt, SyncStatus? syncStatus}) =>
    Task(id: id, title: title ?? this.title, isCompleted: isCompleted ?? this.isCompleted,
      updatedAt: updatedAt ?? this.updatedAt, syncStatus: syncStatus ?? this.syncStatus);
  Map<String, dynamic> toJson() =>
    {'id': id, 'title': title, 'is_completed': isCompleted, 'updated_at': updatedAt.toIso8601String()};
}

class SyncOperation {
  final String id, entityId, action;
  final Map<String, dynamic> payload;
  final DateTime createdAt;
  int retryCount;
  SyncOperation({required this.id, required this.entityId, required this.action,
    required this.payload, required this.createdAt, this.retryCount = 0});
}

class TaskLocalDB {
  final Map<String, Task> _store = {};
  final _changes = StreamController<List<Task>>.broadcast();
  Stream<List<Task>> get changes => _changes.stream;
  void insert(Task t) { _store[t.id] = t; _changes.add(_store.values.toList()); }
  void update(Task t) { _store[t.id] = t; _changes.add(_store.values.toList()); }
  void delete(String id) { _store.remove(id); _changes.add(_store.values.toList()); }
  List<Task> getAll() => _store.values.toList();
  Task? getById(String id) => _store[id];
  void updateStatus(String id, SyncStatus s) {
    final t = _store[id]; if (t != null) { _store[id] = t.copyWith(syncStatus: s); _changes.add(_store.values.toList()); }
  }
}

class SyncQueue {
  final List<SyncOperation> _pending = [];
  final List<SyncOperation> _deadLetter = [];
  final int _maxRetries;
  SyncQueue({int maxRetries = 3}) : _maxRetries = maxRetries;

  void enqueue(SyncOperation op) => _pending.add(op);

  Future<void> processAll(Future<bool> Function(SyncOperation) executor) async {
    for (final op in List.from(_pending)) {
      final ok = await executor(op).catchError((_) => false);
      if (ok) { _pending.remove(op); }
      else { op.retryCount++; if (op.retryCount >= _maxRetries) { _pending.remove(op); _deadLetter.add(op); } }
    }
  }

  int get pendingCount => _pending.length;
  int get deadLetterCount => _deadLetter.length;
}

class TaskRepository {
  final TaskLocalDB _local;
  final SyncQueue _queue;
  final Stream<bool> _connectivity;
  StreamSubscription? _sub;
  bool _isOnline;

  TaskRepository({required TaskLocalDB local, required SyncQueue queue,
    required Stream<bool> connectivity, bool isOnline = true})
      : _local = local, _queue = queue, _connectivity = connectivity, _isOnline = isOnline {
    _sub = _connectivity.listen((online) { _isOnline = online; if (online) syncPending(); });
  }

  Task createTask(String title) {
    final now = DateTime.now();
    final task = Task(id: 'task_${now.millisecondsSinceEpoch}', title: title,
      isCompleted: false, updatedAt: now, syncStatus: SyncStatus.pending);
    _local.insert(task);
    _queue.enqueue(SyncOperation(id: 'op_${now.millisecondsSinceEpoch}',
      entityId: task.id, action: 'create', payload: task.toJson(), createdAt: now));
    if (_isOnline) syncPending();
    return task;
  }

  Future<void> syncPending() async {
    await _queue.processAll((op) async => _isOnline);
    for (final t in _local.getAll().where((t) => t.syncStatus == SyncStatus.pending)) {
      final stillPending = _queue._pending.any((op) => op.entityId == t.id);
      if (!stillPending) _local.updateStatus(t.id, SyncStatus.synced);
    }
  }

  void dispose() { _sub?.cancel(); }
}
```

### Common Mistakes

- **Not handling idempotency**: If a sync succeeds but the response is lost, the client retries and creates a duplicate. Use operation IDs as idempotency keys.
- **Processing out of order**: Create then delete must process in order. FIFO queue handles this naturally.

---

## Exercise 6: WebSocket Real-Time Chat

### Hints

1. `WebSocketChannel.connect(uri)` gives a channel with `.stream` (incoming) and `.sink` (outgoing).
2. Exponential backoff: `min(pow(2, retryCount), 30)` seconds.
3. Outbox: queue messages while disconnected, flush in order on reconnect.

### Solution

```dart
// exercise_06_solution.dart
import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'package:web_socket_channel/web_socket_channel.dart';

enum MessageStatus { sending, sent, delivered, failed }

class ChatMessage {
  final String id, sender, content;
  final DateTime timestamp;
  final MessageStatus status;
  const ChatMessage({required this.id, required this.sender, required this.content,
    required this.timestamp, required this.status});
  ChatMessage copyWith({MessageStatus? status}) =>
    ChatMessage(id: id, sender: sender, content: content, timestamp: timestamp, status: status ?? this.status);
  Map<String, dynamic> toJson() =>
    {'id': id, 'sender': sender, 'content': content, 'timestamp': timestamp.toIso8601String()};
  factory ChatMessage.fromJson(Map<String, dynamic> json) => ChatMessage(
    id: json['id'] as String, sender: json['sender'] as String, content: json['content'] as String,
    timestamp: DateTime.parse(json['timestamp'] as String), status: MessageStatus.delivered);
}

class WebSocketChatClient {
  WebSocketChannel? _channel;
  final String _url;
  final _incoming = StreamController<ChatMessage>.broadcast();
  final _connectionState = StreamController<bool>.broadcast();
  final List<ChatMessage> _outbox = [];
  Timer? _reconnectTimer;
  int _reconnectAttempts = 0;
  bool _intentionalClose = false, _isConnected = false;

  WebSocketChatClient(this._url);
  Stream<ChatMessage> get messages => _incoming.stream;
  Stream<bool> get connectionState => _connectionState.stream;
  bool get isConnected => _isConnected;

  void connect() {
    _intentionalClose = false;
    try {
      _channel = WebSocketChannel.connect(Uri.parse(_url));
      _isConnected = true; _reconnectAttempts = 0;
      _connectionState.add(true);
      _flushOutbox();
      _channel!.stream.listen(
        (data) { try { _incoming.add(ChatMessage.fromJson(jsonDecode(data as String))); } catch (_) {} },
        onError: (_) => _handleDisconnect(),
        onDone: () => _handleDisconnect(),
      );
    } catch (_) { _handleDisconnect(); }
  }

  void sendMessage(ChatMessage msg) {
    if (_isConnected && _channel != null) {
      try { _channel!.sink.add(jsonEncode(msg.toJson())); } catch (_) { _outbox.add(msg); }
    } else { _outbox.add(msg); }
  }

  void _flushOutbox() {
    final batch = List<ChatMessage>.from(_outbox); _outbox.clear();
    for (final msg in batch) {
      try { _channel!.sink.add(jsonEncode(msg.toJson())); } catch (_) { _outbox.add(msg); break; }
    }
  }

  void _handleDisconnect() {
    _isConnected = false; _connectionState.add(false);
    if (!_intentionalClose) _scheduleReconnect();
  }

  void _scheduleReconnect() {
    _reconnectTimer?.cancel();
    final delay = min(pow(2, _reconnectAttempts).toInt(), 30);
    _reconnectAttempts++;
    _reconnectTimer = Timer(Duration(seconds: delay), connect);
  }

  void disconnect() {
    _intentionalClose = true; _reconnectTimer?.cancel();
    _channel?.sink.close(); _isConnected = false; _connectionState.add(false);
  }

  void dispose() { disconnect(); _incoming.close(); _connectionState.close(); }
}
```

### Common Mistakes

- **No `_intentionalClose` flag**: Calling `disconnect()` triggers reconnection. The flag distinguishes user-initiated vs accidental disconnects.
- **Unbounded backoff**: Without `min(..., 30)`, after 20 failures you wait over a million seconds.
- **Flushing outbox out of order**: Use a `List`, not a `Set`. Message order matters for conversations.

---

## Exercise 7: Sync Engine

### Hints

1. `happenedBefore`: all entries in `this` <= `other`, at least one strictly less. Missing keys count as 0.
2. Concurrent + same field + different values = `ManualResolutionNeeded`. Same value = no conflict.
3. Delta: iterate `current` keys, include those that differ from `lastSynced`. Check `lastSynced` keys not in `current` (deleted fields -> null).
4. Batch by entity type. Deletes before creates (priority ordering) to avoid orphans.

### Key Solution Fragments

```dart
// Vector clock comparison
bool happenedBefore(VectorClock other) {
  final allKeys = {..._clocks.keys, ...other._clocks.keys};
  bool atLeastOneLess = false;
  for (final key in allKeys) {
    final thisVal = this[key], otherVal = other[key];
    if (thisVal > otherVal) return false;
    if (thisVal < otherVal) atLeastOneLess = true;
  }
  return atLeastOneLess;
}

// Delta computation
Map<String, dynamic> computeDelta(Map<String, dynamic> current, Map<String, dynamic> lastSynced) {
  final delta = <String, dynamic>{};
  for (final e in current.entries) {
    if (!lastSynced.containsKey(e.key) || lastSynced[e.key] != e.value) delta[e.key] = e.value;
  }
  for (final key in lastSynced.keys) {
    if (!current.containsKey(key)) delta[key] = null; // deleted field
  }
  return delta;
}

// Conflict resolution
ConflictResult resolve(FieldChange local, FieldChange remote) {
  if (local.clock.happenedBefore(remote.clock)) return AutoResolved(remote.newValue, 'Remote is later');
  if (remote.clock.happenedBefore(local.clock)) return AutoResolved(local.newValue, 'Local is later');
  if (local.newValue == remote.newValue) return AutoResolved(local.newValue, 'Same value');
  // Concurrent, different values -> manual
  return ManualResolutionNeeded(localValue: local.newValue, remoteValue: remote.newValue, ...);
}
```

### Common Mistakes

- **Using only timestamps**: Clock skew across devices makes timestamps unreliable. Vector clocks track causal ordering without synchronized clocks.
- **Sending full entities instead of deltas**: Wastes bandwidth on slow connections. Compute the minimal changeset.
- **Not capping exponential backoff**: `pow(2, 20)` is over a million seconds. Always cap at 30-60s.

---

## Exercise 8: GraphQL Normalized Cache

### Hints

1. Walk response recursively. Objects with `__typename` + `id` -> extract to store as `TypeName:id`, replace inline with `CacheReference`.
2. Reading: resolve `CacheReference` back to entity fields. If any reference is missing, return null.
3. `watchQuery`: emit cached data, re-emit when `_updateController` fires with keys overlapping this query's references.

### Key Solution Fragments

```dart
// Normalization
dynamic _normalize(dynamic data, String queryKey, Set<String> changedKeys) {
  if (data is Map<String, dynamic> && data.containsKey('__typename') && data.containsKey('id')) {
    final key = '${data['__typename']}:${data['id']}';
    final fields = <String, dynamic>{};
    for (final e in data.entries) {
      if (e.key != '__typename') fields[e.key] = _normalize(e.value, queryKey, changedKeys);
    }
    _store[key] = CacheEntry(typeName: data['__typename'], id: data['id'].toString(),
        fields: fields, referencedBy: {queryKey});
    changedKeys.add(key);
    return CacheReference(key);
  }
  if (data is Map<String, dynamic>) return data.map((k, v) => MapEntry(k, _normalize(v, queryKey, changedKeys)));
  if (data is List) return data.map((item) => _normalize(item, queryKey, changedKeys)).toList();
  return data;
}

// Denormalization
dynamic _denormalize(dynamic data) {
  if (data is CacheReference) {
    final entry = _store[data.key] ?? (throw StateError('Missing: ${data.key}'));
    return {'__typename': entry.typeName, 'id': entry.id,
      ...entry.fields.map((k, v) => MapEntry(k, _denormalize(v)))};
  }
  if (data is Map<String, dynamic>) return data.map((k, v) => MapEntry(k, _denormalize(v)));
  if (data is List) return data.map(_denormalize).toList();
  return data;
}
```

### Common Mistakes

- **Not recursively normalizing nested objects**: A User containing Posts containing Comments needs all three levels normalized.
- **Forgetting reference tracking**: Without `referencedBy`, garbage collection cannot know which entities are still needed.
- **Circular references during denormalization**: GraphQL responses are trees, but if your schema has cycles, add cycle detection.

---

## General Debugging Tips

- **Log raw request and response** before any parsing. Most networking bugs are obvious in the raw data.
- **Verify Content-Type header** on POST/PUT. Missing it means the server may ignore the body entirely.
- **Test with airplane mode early**. Do not wait until the feature is "done" to discover missing error handling.
- **Use a network proxy** (Charles, mitmproxy) to inspect, delay, and modify traffic in real time.
- **Hot reload preserves widget state**: `initState` does not re-run. If networking code lives there, use hot restart.
