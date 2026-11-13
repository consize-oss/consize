import { redirect } from "next/navigation";
export default function Page() {
  redirect("/recommendations?status=pending_approval");
}
