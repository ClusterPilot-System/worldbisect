import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location("action_mode", pathlib.Path(__file__).with_name("validate-action-mode.py"))
mode = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mode)


class ActionModeTests(unittest.TestCase):
    def test_existing_consumers_keep_compare_mode(self):
        self.assertEqual(mode.validate({"INPUT_COMMAND": "./check", "INPUT_GOOD_WORKSPACE": "good", "INPUT_BAD_WORKSPACE": "bad"}), "compare")

    def test_ci_requires_explicit_selection(self):
        env = {"INPUT_MODE": "ci", "INPUT_COMMAND": '["./check"]', "INPUT_FILES": "check\nconfig"}
        self.assertEqual(mode.validate(env), "ci")
        for changes in ({"INPUT_FILES": ""}, {"INPUT_GOOD_WORKSPACE": "good"}, {"INPUT_FAIL_ON": "proven"}, {"INPUT_REPOSITORY": "other/repo"}):
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                mode.validate(env | changes)

    def test_ambiguous_and_missing_inputs_fail_before_execution(self):
        for env in ({"INPUT_MODE": "unknown"}, {"INPUT_COMMAND": "./check"}, {"INPUT_MODE": "compare", "INPUT_COMMAND": "./check", "INPUT_FILES": "config"}):
            with self.subTest(env=env), self.assertRaises(ValueError):
                mode.validate(env)


if __name__ == "__main__":
    unittest.main()
