import { reactive } from "vue";

export const snackbar = reactive({
  show: false,
  text: "",
  color: "success",
  timeout: 4500
});

export function showSnackbar(text, color = "success", timeout = 4500) {
  snackbar.text = text;
  snackbar.color = color;
  snackbar.timeout = timeout;
  snackbar.show = true;
}

export function showErrorSnackbar(text) {
  showSnackbar(text, "error");
}

export function showSuccessSnackbar(text) {
  showSnackbar(text, "success");
}
