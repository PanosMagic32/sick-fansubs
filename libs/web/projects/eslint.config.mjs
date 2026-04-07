import { defineConfig } from "eslint/config";
import path from "node:path";
import { fileURLToPath } from "node:url";
import js from "@eslint/js";
import { FlatCompat } from "@eslint/eslintrc";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const compat = new FlatCompat({
    baseDirectory: __dirname,
    recommendedConfig: js.configs.recommended,
    allConfig: js.configs.all
});

export default defineConfig([{
    extends: compat.extends("../../../.eslintrc.json"),
}, {
    files: ["**/*.ts"],

    extends: compat.extends(
        "plugin:@nx/angular",
        "plugin:@angular-eslint/template/process-inline-templates",
    ),

    rules: {
        "@angular-eslint/directive-selector": ["error", {
            type: "attribute",
            prefix: "sf",
            style: "camelCase",
        }],

        "@angular-eslint/component-selector": ["error", {
            type: "element",
            prefix: "sf",
            style: "kebab-case",
        }],

        "@angular-eslint/prefer-standalone": "off",
    },
}, {
    files: ["**/*.html"],
    extends: compat.extends("plugin:@nx/angular-template"),
    rules: {},
}]);