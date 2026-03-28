# Section 13: Solutions -- Flutter Forms, User Input & Validation

## How to Use This File

Work through each exercise on your own first. When stuck, follow this progression:
1. Read the **progressive hints** -- each reveals a bit more without the full answer
2. Check **common mistakes** -- your issue might be a known pitfall
3. Study the **full solution** -- understand every line, not just the result
4. Read the **deep dive** -- learn the "why" behind the approach

---

## Exercise 1: Login Form with Validation

### Progressive Hints

1. You need a `GlobalKey<FormState>` as a field on State, not inside `build()`. Creating it in `build()` resets form state on every rebuild.
2. For password visibility, use `bool _obscurePassword = true`. The suffix icon's `onPressed` flips it via `setState`. Pass it to `obscureText`.
3. For the digit requirement, use `RegExp(r'\d').hasMatch(value)`.

### Full Solution

```dart
// file: exercise_1_solution.dart
class _LoginScreenState extends State<LoginScreen> {
  final _formKey = GlobalKey<FormState>();
  final _emailController = TextEditingController();
  final _passwordController = TextEditingController();
  bool _obscurePassword = true;
  bool _isSubmitting = false;

  @override
  void dispose() {
    _emailController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  String? _validateEmail(String? value) {
    if (value == null || value.trim().isEmpty) return 'Email is required';
    if (!RegExp(r'^[\w\-.]+@([\w\-]+\.)+[\w\-]{2,4}$').hasMatch(value.trim())) {
      return 'Enter a valid email address';
    }
    return null;
  }

  String? _validatePassword(String? value) {
    if (value == null || value.isEmpty) return 'Password is required';
    if (value.length < 8) return 'Password must be at least 8 characters';
    if (!RegExp(r'\d').hasMatch(value)) return 'Must contain at least one digit';
    return null;
  }

  Future<void> _handleSubmit() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() => _isSubmitting = true);
    await Future.delayed(const Duration(seconds: 1));
    if (!mounted) return;
    setState(() => _isSubmitting = false);
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('Signed in as ${_emailController.text.trim()}')),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Login')),
      body: Padding(
        padding: const EdgeInsets.all(24.0),
        child: Form(
          key: _formKey,
          autovalidateMode: AutovalidateMode.onUserInteraction,
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              TextFormField(
                controller: _emailController,
                decoration: const InputDecoration(labelText: 'Email', prefixIcon: Icon(Icons.email_outlined)),
                keyboardType: TextInputType.emailAddress,
                textInputAction: TextInputAction.next,
                validator: _validateEmail,
              ),
              const SizedBox(height: 16),
              TextFormField(
                controller: _passwordController,
                decoration: InputDecoration(
                  labelText: 'Password',
                  prefixIcon: const Icon(Icons.lock_outlined),
                  suffixIcon: IconButton(
                    icon: Icon(_obscurePassword ? Icons.visibility_off : Icons.visibility),
                    onPressed: () => setState(() => _obscurePassword = !_obscurePassword),
                  ),
                ),
                obscureText: _obscurePassword,
                validator: _validatePassword,
                onFieldSubmitted: (_) => _handleSubmit(),
              ),
              const SizedBox(height: 24),
              SizedBox(
                width: double.infinity, height: 48,
                child: ElevatedButton(
                  onPressed: _isSubmitting ? null : _handleSubmit,
                  child: _isSubmitting
                      ? const SizedBox(height: 20, width: 20, child: CircularProgressIndicator(strokeWidth: 2))
                      : const Text('Sign In'),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
```

### Common Mistakes

**Creating GlobalKey inside build():** A new key per rebuild loses all form state. It must be a State field.

**Not disposing controllers:** Leaked controllers keep listeners alive, causing "setState called after dispose" crashes.

**Not checking `mounted` after await:** The widget could unmount during the network call. Always guard `setState` after async gaps.

### Deep Dive

Why `GlobalKey<FormState>`? The Form needs to coordinate children deep in the widget tree. A GlobalKey provides direct access to FormState regardless of hierarchy. When `validate()` runs, FormState iterates over every registered FormField child -- the Visitor pattern without fields knowing about each other.

---

## Exercise 2: Contact Form with Multiple Input Types

### Progressive Hints

1. TextFormField has a `maxLength` parameter that auto-adds a counter. To customize the format ("42 / 500"), use the `buildCounter` parameter.
2. Use `DropdownButtonFormField<String>` so the dropdown integrates with Form validation.
3. `_formKey.currentState!.reset()` resets FormFields but NOT your plain State variables like `_priority` or `_subscribe`. Reset those manually in `setState`.

### Full Solution

```dart
// file: exercise_2_solution.dart
class _ContactFormScreenState extends State<ContactFormScreen> {
  final _formKey = GlobalKey<FormState>();
  final _messageController = TextEditingController();
  String? _category;
  String _priority = 'Medium';
  bool _subscribe = false;

  @override
  void initState() {
    super.initState();
    _messageController.addListener(() => setState(() {}));
  }

  @override
  void dispose() { _messageController.dispose(); super.dispose(); }

  void _resetForm() {
    _formKey.currentState!.reset();
    _messageController.clear();
    setState(() { _category = null; _priority = 'Medium'; _subscribe = false; });
  }

  void _submitForm() {
    if (!_formKey.currentState!.validate()) return;
    _formKey.currentState!.save();
    debugPrint('Category: $_category, Priority: $_priority, Subscribe: $_subscribe');
    debugPrint('Message: ${_messageController.text}');
  }

  @override
  Widget build(BuildContext context) {
    return Form(
      key: _formKey,
      autovalidateMode: AutovalidateMode.onUserInteraction,
      child: ListView(padding: const EdgeInsets.all(16), children: [
        TextFormField(
          decoration: const InputDecoration(labelText: 'Name'),
          validator: (v) => (v == null || v.trim().isEmpty) ? 'Required' : null,
        ),
        const SizedBox(height: 16),
        TextFormField(
          decoration: const InputDecoration(labelText: 'Email'),
          keyboardType: TextInputType.emailAddress,
          validator: (v) => (v == null || !v.contains('@')) ? 'Valid email required' : null,
        ),
        const SizedBox(height: 16),
        DropdownButtonFormField<String>(
          decoration: const InputDecoration(labelText: 'Category'),
          value: _category,
          items: ['Bug Report', 'Feature Request', 'Account Issue', 'Other']
              .map((c) => DropdownMenuItem(value: c, child: Text(c))).toList(),
          onChanged: (v) => setState(() => _category = v),
          validator: (v) => v == null ? 'Select a category' : null,
        ),
        const SizedBox(height: 16),
        const Text('Priority', style: TextStyle(fontWeight: FontWeight.w500)),
        ...['Low', 'Medium', 'High'].map((p) => RadioListTile<String>(
          title: Text(p), value: p, groupValue: _priority,
          onChanged: (v) => setState(() => _priority = v!),
        )),
        TextFormField(
          controller: _messageController,
          decoration: const InputDecoration(labelText: 'Message', alignLabelWithHint: true),
          maxLines: 5, maxLength: 500,
          buildCounter: (ctx, {required currentLength, required isFocused, required maxLength}) =>
              Text('$currentLength / $maxLength'),
          validator: (v) {
            if (v == null || v.trim().isEmpty) return 'Required';
            if (v.trim().length < 20) return 'At least 20 characters';
            return null;
          },
        ),
        CheckboxListTile(
          title: const Text('Subscribe to updates'), value: _subscribe,
          onChanged: (v) => setState(() => _subscribe = v ?? false),
        ),
        Row(children: [
          Expanded(child: OutlinedButton(onPressed: _resetForm, child: const Text('Reset'))),
          const SizedBox(width: 16),
          Expanded(child: ElevatedButton(onPressed: _submitForm, child: const Text('Submit'))),
        ]),
      ]),
    );
  }
}
```

### Deep Dive

RadioListTile has no FormField variant in the standard library, so it does not participate in `validate()` or `reset()`. If you need radio validation, options are: (1) wrap in a custom FormField, (2) validate manually in submit, or (3) use `flutter_form_builder`. Here, since radio has a default, validation is unnecessary.

---

## Exercise 3: Multi-Step Registration Wizard

### Progressive Hints

1. Use a separate `GlobalKey<FormState>` per step. Sharing one key across steps loses validation state when the Form unmounts.
2. Store all field values as State variables on the parent widget. Each step reads from and writes to them -- this is how data persists across navigation.
3. For confirm password validation, read `_passwordController.text` inside the validator closure.
4. For age: compare year, month, and day rather than just subtracting years (leap year edge case).

### Full Solution

```dart
// file: exercise_3_solution.dart
class _RegistrationWizardState extends State<RegistrationWizard> {
  int _currentStep = 0;
  final _stepKeys = List.generate(3, (_) => GlobalKey<FormState>());
  final _passwordController = TextEditingController();
  final _confirmController = TextEditingController();
  final _emailController = TextEditingController();
  String _firstName = '', _lastName = '';
  DateTime? _birthDate;
  String? _gender, _language;
  bool _notifications = true, _newsletter = false;

  @override
  void dispose() {
    _passwordController.dispose();
    _confirmController.dispose();
    _emailController.dispose();
    super.dispose();
  }

  bool _isAtLeast13(DateTime d) {
    final now = DateTime.now();
    var age = now.year - d.year;
    if (now.month < d.month || (now.month == d.month && now.day < d.day)) age--;
    return age >= 13;
  }

  void _next() {
    if (_stepKeys[_currentStep].currentState?.validate() != true) return;
    _stepKeys[_currentStep].currentState!.save();
    if (_currentStep < 2) { setState(() => _currentStep++); } else { _showSummary(); }
  }

  void _back() { if (_currentStep > 0) setState(() => _currentStep--); }

  void _showSummary() {
    showDialog(context: context, builder: (ctx) => AlertDialog(
      title: const Text('Summary'),
      content: Text('Email: ${_emailController.text}\nName: $_firstName $_lastName\n'
          'Birth: ${_birthDate?.toIso8601String().split("T").first}\nGender: $_gender\n'
          'Notifications: $_notifications\nNewsletter: $_newsletter\nLanguage: $_language'),
      actions: [TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('OK'))],
    ));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Registration')),
      body: Column(children: [
        // Step indicator
        Padding(padding: const EdgeInsets.all(16), child: Row(
          children: List.generate(3, (i) => Expanded(child: Container(
            height: 4, margin: const EdgeInsets.symmetric(horizontal: 2),
            color: i <= _currentStep ? Colors.blue : Colors.grey.shade300,
          ))),
        )),
        Expanded(child: SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: [_buildStep0(), _buildStep1(), _buildStep2()][_currentStep],
        )),
        Padding(padding: const EdgeInsets.all(16), child: Row(children: [
          if (_currentStep > 0) Expanded(child: OutlinedButton(onPressed: _back, child: const Text('Back'))),
          if (_currentStep > 0) const SizedBox(width: 16),
          Expanded(child: ElevatedButton(onPressed: _next, child: Text(_currentStep == 2 ? 'Submit' : 'Next'))),
        ])),
      ]),
    );
  }

  Widget _buildStep0() => Form(key: _stepKeys[0], autovalidateMode: AutovalidateMode.onUserInteraction, child: Column(children: [
    TextFormField(controller: _emailController, decoration: const InputDecoration(labelText: 'Email'),
      validator: (v) => (v == null || !v.contains('@')) ? 'Valid email required' : null),
    const SizedBox(height: 16),
    TextFormField(controller: _passwordController, obscureText: true, decoration: const InputDecoration(labelText: 'Password'),
      validator: (v) => (v == null || v.length < 8) ? 'At least 8 characters' : null),
    const SizedBox(height: 16),
    TextFormField(controller: _confirmController, obscureText: true, decoration: const InputDecoration(labelText: 'Confirm Password'),
      validator: (v) => v != _passwordController.text ? 'Passwords do not match' : null),
  ]));

  Widget _buildStep1() => Form(key: _stepKeys[1], autovalidateMode: AutovalidateMode.onUserInteraction, child: Column(children: [
    TextFormField(initialValue: _firstName, decoration: const InputDecoration(labelText: 'First Name'),
      validator: (v) => (v == null || v.trim().isEmpty) ? 'Required' : null, onSaved: (v) => _firstName = v?.trim() ?? ''),
    const SizedBox(height: 16),
    TextFormField(initialValue: _lastName, decoration: const InputDecoration(labelText: 'Last Name'),
      validator: (v) => (v == null || v.trim().isEmpty) ? 'Required' : null, onSaved: (v) => _lastName = v?.trim() ?? ''),
    const SizedBox(height: 16),
    ListTile(
      title: const Text('Birth Date'),
      subtitle: Text(_birthDate != null ? _birthDate!.toIso8601String().split('T').first : 'Tap to select'),
      trailing: const Icon(Icons.calendar_today),
      onTap: () async {
        final picked = await showDatePicker(context: context, initialDate: DateTime(2000), firstDate: DateTime(1900), lastDate: DateTime.now());
        if (picked != null && !_isAtLeast13(picked)) {
          if (mounted) ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Must be at least 13 years old')));
          return;
        }
        if (picked != null) setState(() => _birthDate = picked);
      },
    ),
    DropdownButtonFormField<String>(decoration: const InputDecoration(labelText: 'Gender'), value: _gender,
      items: ['Male','Female','Non-binary','Prefer not to say'].map((g) => DropdownMenuItem(value: g, child: Text(g))).toList(),
      onChanged: (v) => setState(() => _gender = v)),
  ]));

  Widget _buildStep2() => Form(key: _stepKeys[2], child: Column(children: [
    SwitchListTile(title: const Text('Notifications'), value: _notifications, onChanged: (v) => setState(() => _notifications = v)),
    CheckboxListTile(title: const Text('Newsletter'), value: _newsletter, onChanged: (v) => setState(() => _newsletter = v ?? false)),
    DropdownButtonFormField<String>(decoration: const InputDecoration(labelText: 'Language'), value: _language,
      items: ['English','Spanish','French','German'].map((l) => DropdownMenuItem(value: l, child: Text(l))).toList(),
      onChanged: (v) => setState(() => _language = v)),
  ]));
}
```

### Common Mistakes

**Using controller AND initialValue together:** TextFormField rejects both. Use `initialValue` for save-only fields; use controllers when you need to read values in real-time (like password for cross-field validation).

**Using `ValueKey` on step dropdowns:** When the country changes in step 2 and the dropdown items change, Flutter may reuse the old widget with stale internal state. Add `key: ValueKey(_selectedCountry)` to force a rebuild.

---

## Exercise 4: Async Username Validator with Debounce

### Progressive Hints

1. Store the Timer as a field. Cancel it on every keystroke -- otherwise typing "admin" fires five server calls.
2. After async completes, compare `_controller.text` with the value you checked. The user may have typed more.
3. Surface `_asyncError` through the synchronous validator: `if (_asyncError != null) return _asyncError;`
4. Use `FilteringTextInputFormatter.allow(RegExp(r'[a-zA-Z0-9_]'))` to block invalid characters.

### Full Solution

```dart
// file: exercise_4_solution.dart
class _UsernameRegistrationState extends State<UsernameRegistration> {
  final _formKey = GlobalKey<FormState>();
  final _usernameController = TextEditingController();
  Timer? _debounceTimer;
  bool _isChecking = false;
  bool? _isAvailable;
  String? _asyncError;
  final _taken = ['admin', 'user', 'test', 'flutter', 'dart'];

  @override
  void initState() { super.initState(); _usernameController.addListener(_onChanged); }

  void _onChanged() {
    _debounceTimer?.cancel();
    final name = _usernameController.text.trim();
    if (name.length < 3 || !RegExp(r'^[a-zA-Z0-9_]+$').hasMatch(name)) {
      setState(() { _isChecking = false; _isAvailable = null; _asyncError = null; });
      return;
    }
    setState(() { _isChecking = true; _isAvailable = null; _asyncError = null; });
    _debounceTimer = Timer(const Duration(milliseconds: 600), () => _check(name));
  }

  Future<void> _check(String name) async {
    await Future.delayed(const Duration(milliseconds: 800));
    if (!mounted || _usernameController.text.trim() != name) return;
    final isTaken = _taken.contains(name.toLowerCase());
    setState(() { _isChecking = false; _isAvailable = !isTaken; _asyncError = isTaken ? '"$name" is taken' : null; });
  }

  @override
  void dispose() { _debounceTimer?.cancel(); _usernameController.dispose(); super.dispose(); }

  @override
  Widget build(BuildContext context) {
    return Scaffold(body: Padding(padding: const EdgeInsets.all(24), child: Form(key: _formKey,
      autovalidateMode: AutovalidateMode.onUserInteraction, child: Column(children: [
        TextFormField(
          controller: _usernameController,
          decoration: InputDecoration(labelText: 'Username',
            suffixIcon: _isChecking ? const Padding(padding: EdgeInsets.all(12), child: SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2)))
                : _isAvailable == true ? const Icon(Icons.check_circle, color: Colors.green)
                : _isAvailable == false ? const Icon(Icons.cancel, color: Colors.red) : null,
            helperText: _isAvailable == true ? 'Available' : null, helperStyle: const TextStyle(color: Colors.green),
          ),
          inputFormatters: [FilteringTextInputFormatter.allow(RegExp(r'[a-zA-Z0-9_]')), LengthLimitingTextInputFormatter(20)],
          validator: (v) {
            if (v == null || v.trim().isEmpty) return 'Required';
            if (v.trim().length < 3) return 'At least 3 characters';
            if (_asyncError != null) return _asyncError;
            if (_isChecking) return 'Checking...';
            return null;
          },
        ),
        const SizedBox(height: 16),
        TextFormField(decoration: const InputDecoration(labelText: 'Display Name'),
          validator: (v) => (v == null || v.trim().isEmpty) ? 'Required' : null),
        const SizedBox(height: 16),
        TextFormField(decoration: const InputDecoration(labelText: 'Email'), keyboardType: TextInputType.emailAddress,
          validator: (v) => (v == null || !v.contains('@')) ? 'Valid email required' : null),
        const SizedBox(height: 24),
        ElevatedButton(onPressed: _isChecking ? null : () {
          if (_formKey.currentState!.validate()) debugPrint('Submitted: ${_usernameController.text}');
        }, child: const Text('Create Account')),
      ]))));
  }
}
```

### Common Mistakes

**Not cancelling the previous timer:** Every keystroke adds a timer, all of them fire. Typing "admin" triggers five server calls instead of one.

**Not re-checking the field value after async:** The user might have typed "admin123" while you were checking "admin". Always compare current text with the checked value.

### Deep Dive

Debouncing waits until activity stops, then fires once. Throttling fires at most once per interval regardless of activity. For text validation, debouncing is correct because you want the final value. Throttling suits continuous events like scroll tracking.

---

## Exercise 5: Reusable Form Field Library

### Progressive Hints

1. For the required asterisk, use `InputDecoration.label` (Widget, not String) with a `Row` containing the label and a red `Text(' *')`.
2. For searchable dropdown, use `showDialog` with a `TextField` + filtered `ListView.builder`. Simpler than `OverlayEntry`.
3. AppDateField: `TextFormField` with `readOnly: true` and `onTap` calling `showDatePicker`.
4. AppPhoneField: `Row` with a small `DropdownButton` for country code and an `Expanded` `TextFormField` with a formatting `TextInputFormatter`.

### Common Mistakes

**Creating TextEditingController in a StatelessWidget build:** For read-only display fields (like AppDateField), creating a controller inline works because it is never listened to. But for editable fields, the controller must live in a StatefulWidget to persist across rebuilds.

**Not passing `key: ValueKey(value)` on searchable dropdown's display:** When the selected value changes externally (e.g., "same as billing"), the InputDecorator might not rebuild. Force it with a ValueKey.

### Deep Dive

The searchable dropdown pattern (dialog with filter) is the simplest approach. Alternatives include `Autocomplete` widget (built-in, renders inline), `OverlayEntry` (custom positioning, more control), or packages like `dropdown_search`. The dialog approach is most reliable across platforms because it does not fight with keyboard insets or scroll containers.

---

## Exercise 6: Searchable Dropdown with Cross-Field Validation

### Progressive Hints

1. Structure data as `Map<String, List<String>>` for country-to-states and `Map<String, RegExp>` for postal patterns.
2. When country changes, clear `_selectedState` AND add `key: ValueKey(_selectedCountry)` on the state dropdown to force rebuild.
3. For highlighted text, use `RichText` with `TextSpan` children: split the string around the match, apply bold + yellow background to the matching portion.
4. Re-validate postal code when country changes: `WidgetsBinding.instance.addPostFrameCallback((_) => _formKey.currentState?.validate())`.

### Common Mistakes

**DropdownButtonFormField crashes when value is not in items:** When switching countries, the old state value no longer exists in the new items list. Always set `_selectedState = null` before the items change.

**Postal validation shows old country's rules:** The validator captures `_selectedCountry` from the closure. If you change the country but do not re-run validation, the postal field shows stale errors. Trigger re-validation with `addPostFrameCallback`.

### Deep Dive

Cross-field validation is inherently stateful. Each validator only receives its own value. Access other fields via: (1) read controllers/state variables from the closure (simplest), (2) create custom FormFields that accept dependent values, or (3) use `reactive_forms` for explicit dependency modeling. For most apps, option 1 suffices.

---

## Exercise 7: Schema-Driven Dynamic Form Generator

### Progressive Hints

1. Parse JSON into typed classes (`FormSchema`, `FieldDefinition`, `StepDefinition`) immediately. Working with raw `Map<String, dynamic>` throughout is fragile.
2. Map field types to builders: `switch (field.type) { case 'text': return _buildTextField(field); ... }`.
3. Store form data in `Map<String, dynamic>`. Check conditions during `build()`: if `field.condition.dependsOn` value does not match, skip rendering.
4. Array fields: use `List<Map<String, dynamic>>` per array key. Render items in a list with Add/Remove buttons enforcing min/max.
5. Use `onChanged` (not just `onSaved`) so values persist when navigating between steps.

### Common Mistakes

**Using `initialValue` for fields whose data changes externally:** When a "same as billing" fills fields, `initialValue` was already read during `initState`. Use controllers that you update programmatically.

**Not adding `ValueKey(field.key)` to dynamic fields:** Flutter may reuse a widget for the wrong field when conditional visibility changes the rendered set. Always key dynamic fields.

**Array items losing data on add/remove:** If you rebuild the array list without preserving existing data, previously entered values disappear. Store item data in the list and use indexed access.

### Debugging Tips

If field values disappear between steps, check that `onChanged` writes to `_formData` immediately. The `onSaved` callback only fires on explicit `save()` -- users expect typed text to persist on "Back".

---

## Exercise 8: Rich Text Editor with Formatting Toolbar

### Progressive Hints

1. Build a document model: `List<StyledSpan>` where each span has `String text` and `Set<RichStyle> styles`.
2. Undo/redo: two stacks (`List<DocumentModel>`). Every mutation pushes previous state to undo, clears redo. Cap at 50.
3. Keyboard shortcuts: `Focus` widget with `onKeyEvent`. Check `HardwareKeyboard.instance.logicalKeysPressed` for Ctrl/Cmd.
4. Style toggling: split affected spans at selection boundaries, flip the style on the selected portion, then merge adjacent spans with identical styles.
5. Markdown export: iterate spans, wrap text in `**`/`*`/`~~` based on active styles.

### Common Mistakes

**Trying to show styled text in a TextField:** Standard TextField renders plain text only. You need either a custom `EditableText` with `TextSpan` builder, or show styles only in preview mode (the simpler approach used here).

**Unbounded undo stack:** Without a cap, heavy editing consumes significant memory. Always enforce a maximum (50 is reasonable).

**Confusing KeyDownEvent with KeyRepeatEvent:** Handling only `KeyDownEvent` means holding Ctrl+Z fires undo once. Handle `KeyRepeatEvent` too if you want repeat-while-held behavior.

### Deep Dive

A production rich text editor is one of the hardest problems in UI development. The core challenge is the bidirectional mapping between visual cursor position and the styled document model. When the user types inside a bold span, the new character should inherit bold. At the boundary between bold and normal, you need a rule for which style wins. This exercise teaches the concepts, but for production, consider `flutter_quill`, `super_editor`, or `appflowy_editor`.

### Alternatives

Instead of a flat span list, you could use a tree of nodes (paragraphs containing inline spans), similar to ProseMirror or Slate.js. Tree structure makes block-level operations (headings, lists) more natural, since a flat list mixes block and inline concepts.

---

## General Debugging Tips

1. **"setState called after dispose":** Check `if (mounted)` after every `await` before calling `setState`.
2. **"No Form widget found":** `TextFormField` requires a `Form` ancestor. Use `TextField` for standalone inputs.
3. **Validation not showing errors:** Check `autovalidateMode`. With `.disabled`, errors only appear after explicit `validate()`.
4. **Dropdown value not in items:** The `value` must exactly `==` one of the items' values. After rebuilding items, ensure the selected value still exists.
5. **Focus not advancing:** Verify `textInputAction: TextInputAction.next` and that `onFieldSubmitted` calls `nextFocusNode.requestFocus()`.
6. **Form.reset() not clearing controllers:** `reset()` only resets FormField widgets to `initialValue`. Call `controller.clear()` manually.
