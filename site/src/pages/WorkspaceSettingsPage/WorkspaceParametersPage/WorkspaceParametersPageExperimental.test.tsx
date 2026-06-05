import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { act } from "react";
import { API } from "#/api/api";
import {
	MockPreviewParameter1,
	MockPreviewParameter2,
	MockPreviewParameter7,
	MockStoppedWorkspace,
	MockTemplateVersionParameter1,
	MockTemplateVersionParameter2,
	MockTemplateVersionParameter3,
	MockTemplateVersionParameter4,
	MockTemplateVersionParameter7,
	MockWorkspaceBuildParameter1,
	MockWorkspaceBuildParameter7,
} from "#/testHelpers/entities";
import {
	renderWithWorkspaceSettingsLayout,
	waitForLoaderToBeRemoved,
} from "#/testHelpers/renderHelpers";
import { mockDynamicParameterWebSocket } from "#/testHelpers/websockets";
import WorkspaceParametersPageExperimental from "./WorkspaceParametersPageExperimental";

describe("WorkspaceParametersPageExperimental", () => {
	const renderWorkspaceParametersPageExperimental = (
		route = `/@${MockStoppedWorkspace.owner_name}/${MockStoppedWorkspace.name}/settings`,
	) => {
		return renderWithWorkspaceSettingsLayout(
			<WorkspaceParametersPageExperimental />,
			{
				route,
				path: "/:username/:workspace/settings",
				extraRoutes: [
					{
						// Need this because after submit the user is redirected.
						path: "/:username/:workspace",
						element: <div>Workspace Page</div>,
					},
				],
			},
		);
	};

	beforeEach(() => {
		vi.clearAllMocks();
		vi.spyOn(API, "getWorkspaceByOwnerAndName").mockResolvedValueOnce(
			MockStoppedWorkspace,
		);
		vi.spyOn(API, "getTemplateVersionRichParameters").mockResolvedValueOnce([
			MockTemplateVersionParameter1, // required string
			MockTemplateVersionParameter2, // required number
			MockTemplateVersionParameter3, // required string
			MockTemplateVersionParameter4, // required immutable string
			MockTemplateVersionParameter7, // optional string
		]);
		vi.spyOn(API, "postWorkspaceBuild").mockRejectedValueOnce(
			new Error("not implemented"),
		);
	});

	afterEach(() => {
		vi.useRealTimers();
		vi.restoreAllMocks();
	});

	it("does not clobber user input", async () => {
		vi.spyOn(API, "getWorkspaceBuildParameters").mockResolvedValueOnce([]);

		const [, mockPublisher] = mockDynamicParameterWebSocket([
			MockPreviewParameter1,
			MockPreviewParameter7,
		]);

		renderWorkspaceParametersPageExperimental();
		await waitForLoaderToBeRemoved();

		// Remove one value and fill out the other. Make sure the removal is first
		// to test that blank values are preserved and not just being sent once
		// due to being the last modified.
		const form = screen.getByTestId("form");
		const input = await within(form).findByRole("textbox", {
			name: new RegExp(MockPreviewParameter1.display_name, "i"),
		});
		await userEvent.clear(input);
		const input2 = await within(form).findByRole("textbox", {
			name: new RegExp(MockPreviewParameter7.display_name, "i"),
		});
		await userEvent.clear(input2);
		await userEvent.type(input2, "hi there hello");

		// Simulate a slow/stale response.
		await act(async () => {
			mockPublisher.publishMessage(
				new MessageEvent("message", {
					data: JSON.stringify({
						id: 2,
						parameters: [
							MockPreviewParameter1,
							MockPreviewParameter7,
							// Add a new field to test the message is actually received.
							MockPreviewParameter2,
						],
					}),
				}),
			);
		});

		// The last message from the client should not use the stale values.
		await waitFor(() => {
			const lastMessage =
				mockPublisher.clientSentData[mockPublisher.clientSentData.length - 1];
			expect(lastMessage).toBeDefined();
			expect(JSON.parse(lastMessage as string)).toEqual(
				expect.objectContaining({
					inputs: {
						[MockPreviewParameter1.name]: "",
						[MockPreviewParameter2.name]: MockPreviewParameter2.value.value,
						[MockPreviewParameter7.name]: "hi there hello",
					},
				}),
			);
		});

		// The touched fields on the page should not have been updated.
		await waitFor(() => {
			const field1 = within(form).getByTestId(
				`parameter-field-${MockPreviewParameter1.name}`,
			);
			expect(within(field1).getByDisplayValue("")).toBeInTheDocument();

			const field2 = within(form).getByTestId(
				`parameter-field-${MockPreviewParameter7.name}`,
			);
			expect(
				within(field2).getByDisplayValue("hi there hello"),
			).toBeInTheDocument();

			const field3 = within(form).getByTestId(
				`parameter-field-${MockPreviewParameter2.name}`,
			);
			expect(
				within(field3).getByDisplayValue(MockPreviewParameter2.value.value),
			).toBeInTheDocument();
		});

		// Respond with the matching values to unblock the form.
		await act(async () => {
			mockPublisher.publishMessage(
				new MessageEvent("message", {
					data: JSON.stringify({
						id: 3,
						parameters: [
							{
								...MockPreviewParameter1,
								value: { valid: true, value: "" },
								required: false,
							},
							{
								...MockPreviewParameter7,
								value: { valid: true, value: "hi there hello" },
							},
							MockPreviewParameter2,
						],
					}),
				}),
			);
		});

		// Submit the form.
		const submitButton = within(form).getByRole("button", {
			name: /update and start/i,
		});
		await waitFor(() => expect(submitButton).toBeEnabled());
		await userEvent.click(submitButton);

		// Again the touched fields should be used they are.
		await waitFor(() => {
			expect(API.postWorkspaceBuild).toHaveBeenCalledWith(
				MockStoppedWorkspace.id,
				expect.objectContaining({
					transition: "start",
					template_version_id: undefined,
					rich_parameter_values: [
						expect.objectContaining({
							name: MockPreviewParameter1.name,
							value: "",
						}),
						expect.objectContaining({
							name: MockPreviewParameter7.name,
							value: "hi there hello",
						}),
						expect.objectContaining({
							name: MockPreviewParameter2.name,
							value: MockPreviewParameter2.value.value,
						}),
					],
					reason: "dashboard",
				}),
			);
		});
	});

	it("does not clobber auto-filled values", async () => {
		// The "auto-fill" in this case is being provided by the workspace build
		// parameters endpoint.
		vi.spyOn(API, "getWorkspaceBuildParameters").mockResolvedValueOnce([
			MockWorkspaceBuildParameter1,
			MockWorkspaceBuildParameter7,
		]);

		const [, mockPublisher] = mockDynamicParameterWebSocket([
			MockPreviewParameter1,
			MockPreviewParameter7, // tests a blank value
		]);

		renderWorkspaceParametersPageExperimental();
		await waitForLoaderToBeRemoved();

		// Simulate a slow/stale response.
		await act(async () => {
			mockPublisher.publishMessage(
				new MessageEvent("message", {
					data: JSON.stringify({
						id: 2,
						parameters: [
							MockPreviewParameter1,
							MockPreviewParameter7,
							// Add a new field to test the message is actually received.
							MockPreviewParameter2,
						],
					}),
				}),
			);
		});

		// The initial message from the client should not use the stale values.
		await waitFor(() => {
			const lastMessage =
				mockPublisher.clientSentData[mockPublisher.clientSentData.length - 1];
			expect(lastMessage).toBeDefined();
			expect(JSON.parse(lastMessage as string)).toEqual(
				expect.objectContaining({
					inputs: {
						[MockPreviewParameter1.name]: MockWorkspaceBuildParameter1.value,
						[MockPreviewParameter7.name]: MockWorkspaceBuildParameter7.value,
					},
				}),
			);
		});

		// The auto-filled fields on the page should not have been updated.
		const form = screen.getByTestId("form");
		await waitFor(() => {
			const field1 = within(form).getByTestId(
				`parameter-field-${MockPreviewParameter1.name}`,
			);
			expect(
				within(field1).getByDisplayValue(MockWorkspaceBuildParameter1.value),
			).toBeInTheDocument();

			const field2 = within(form).getByTestId(
				`parameter-field-${MockPreviewParameter7.name}`,
			);
			expect(
				within(field2).getByDisplayValue(MockWorkspaceBuildParameter7.value),
			).toBeInTheDocument();

			const field3 = within(form).getByTestId(
				`parameter-field-${MockPreviewParameter2.name}`,
			);
			expect(
				within(field3).getByDisplayValue(MockPreviewParameter2.value.value),
			).toBeInTheDocument();
		});

		// Respond with the matching values to unblock the form.
		await act(async () => {
			mockPublisher.publishMessage(
				new MessageEvent("message", {
					data: JSON.stringify({
						id: 3,
						parameters: [
							{
								...MockPreviewParameter1,
								value: {
									valid: true,
									value: MockWorkspaceBuildParameter1.value,
								},
								required: false,
							},
							{
								...MockPreviewParameter7,
								value: {
									valid: true,
									value: MockWorkspaceBuildParameter7.value,
								},
							},
							MockPreviewParameter2,
						],
					}),
				}),
			);
		});

		// Submit the form.
		const submitButton = within(form).getByRole("button", {
			name: /update and start/i,
		});
		await waitFor(() => expect(submitButton).toBeEnabled());
		await userEvent.click(submitButton);

		// Again the touched fields should be used they are.
		await waitFor(() => {
			expect(API.postWorkspaceBuild).toHaveBeenCalledWith(
				MockStoppedWorkspace.id,
				expect.objectContaining({
					transition: "start",
					template_version_id: undefined,
					rich_parameter_values: [
						expect.objectContaining({
							name: MockPreviewParameter1.name,
							value: MockWorkspaceBuildParameter1.value,
						}),
						expect.objectContaining({
							name: MockPreviewParameter7.name,
							value: MockWorkspaceBuildParameter7.value,
						}),
						expect.objectContaining({
							name: MockPreviewParameter2.name,
							value: MockPreviewParameter2.value.value,
						}),
					],
					reason: "dashboard",
				}),
			);
		});
	});
});
